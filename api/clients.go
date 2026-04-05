package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/lib/pq"
)

type Client struct {
	ID            int                    `json:"id"`
	Name          string                 `json:"name"`
	Email         string                 `json:"email"`
	Phone         string                 `json:"phone"`
	Source        string                 `json:"source"`
	SourceStageID *int                   `json:"source_stage_id,omitempty"`
	Status        string                 `json:"status"` // "active", "lost", "initial_call_scheduled", "follow_up_scheduled", "awaiting_response", "inactive"
	CompletedAt   *time.Time             `json:"completed_at,omitempty"`
	Comments      []CommentCreateRequest `json:"comments,omitempty"`
}

func (h *Handler) validateClientCompletedAt(ctx context.Context, clientID int, completedAt *time.Time) error {
	if completedAt == nil {
		return nil
	}

	completedDay := completedAt.UTC().Truncate(24 * time.Hour)

	var clientCreatedAt sql.NullTime
	if err := h.DB.QueryRowContext(ctx, `SELECT created_at FROM clients WHERE id = $1`, clientID).Scan(&clientCreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("client not found")
		}
		return err
	}
	if clientCreatedAt.Valid {
		createdDay := clientCreatedAt.Time.UTC().Truncate(24 * time.Hour)
		if completedDay.Before(createdDay) {
			return fmt.Errorf("completed_at cannot be before client creation date")
		}
	}

	var followUpDate sql.NullTime
	if err := h.DB.QueryRowContext(ctx, `
		SELECT follow_up_date
		FROM sales_process
		WHERE client_id = $1
		ORDER BY id DESC
		LIMIT 1
	`, clientID).Scan(&followUpDate); err != nil && err != sql.ErrNoRows {
		return err
	}
	if followUpDate.Valid {
		followUpDay := followUpDate.Time.UTC().Truncate(24 * time.Hour)
		if completedDay.Before(followUpDay) {
			return fmt.Errorf("completed_at cannot be before follow_up_date")
		}
	}

	return nil
}

// GET /api/clients
func (h *Handler) ListClients(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	type ClientResponse struct {
		ID              int64             `json:"id"`
		LeadID          *int64            `json:"lead_id,omitempty"`
		Name            string            `json:"name"`
		Email           string            `json:"email"`
		Phone           string            `json:"phone"`
		Source          string            `json:"source"`
		SourceStageName string            `json:"source_stage_name"`
		Status          string            `json:"status"`
		CompletedAt     *string           `json:"completed_at,omitempty"`
		Comments        []CommentResponse `json:"comments,omitempty"`
	}

	includeInactive := r.URL.Query().Get("include_inactive") == "true"

	rows, err := h.DB.QueryContext(ctx, `
WITH client_status AS (
  SELECT
    c.id,
    (
      SELECT l.id
      FROM leads l
      WHERE l.converted_client_id = c.id
      ORDER BY l.converted_at DESC NULLS LAST, l.id DESC
      LIMIT 1
    ) AS lead_id,
    c.name,
    c.email,
    c.phone,
    c.source,
    COALESCE(s.name, '') AS source_stage_name,
    CASE
      WHEN EXISTS (
        SELECT 1
        FROM contracts ct
        WHERE ct.client_id = c.id
          AND (ct.end_date IS NULL OR ct.end_date >= CURRENT_DATE)
      ) THEN 'active'
      WHEN EXISTS (
        SELECT 1 FROM contracts ct WHERE ct.client_id = c.id
      ) THEN 'inactive'
      WHEN c.status = 'inactive' THEN 'inactive'
      WHEN c.status = 'lost' THEN 'lost'
      WHEN c.status IS NOT NULL AND c.status <> 'active' THEN c.status
      ELSE
        CASE
          WHEN sp.stage = 'closed' AND COALESCE(sp.closed, FALSE) = TRUE THEN 'inactive'
          WHEN sp.stage = 'lost' THEN 'lost'
          WHEN sp.stage = 'initial_contact'
            AND sp.initial_contact_date IS NOT NULL
            AND sp.follow_up_result IS NULL
            THEN 'initial_call_scheduled'
          WHEN sp.stage = 'follow_up'
            AND sp.follow_up_result IS NULL
            THEN 'follow_up_scheduled'
          WHEN sp.stage = 'follow_up'
            AND sp.follow_up_result IS TRUE
            THEN 'awaiting_response'
          ELSE 'inactive'
        END
    END AS status,
    c.completed_at
  FROM clients c
  LEFT JOIN stages s ON s.id = c.source_stage_id
  LEFT JOIN sales_process sp ON sp.id = (
    SELECT sp2.id
    FROM sales_process sp2
    WHERE sp2.client_id = c.id
    ORDER BY sp2.id DESC
    LIMIT 1
  )
)
SELECT * FROM client_status
WHERE ($1 = (status IN ('inactive', 'lost')))
ORDER BY id
`, includeInactive)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	clients := make([]ClientResponse, 0, 64)
	clientIDs := make([]int64, 0, 64)
	idToIndex := make(map[int64]int)

	for rows.Next() {
		var c ClientResponse
		var leadID sql.NullInt64
		var completedAt sql.NullTime
		var emailNS, phoneNS, sourceNS sql.NullString

		if err := rows.Scan(
			&c.ID, &leadID, &c.Name, &emailNS,
			&phoneNS, &sourceNS, &c.SourceStageName, &c.Status, &completedAt,
		); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if leadID.Valid {
			id := leadID.Int64
			c.LeadID = &id
		}

		if emailNS.Valid {
			c.Email = emailNS.String
		}
		if phoneNS.Valid {
			c.Phone = phoneNS.String
		}
		if sourceNS.Valid {
			c.Source = sourceNS.String
		}
		if completedAt.Valid {
			date := completedAt.Time.Format("2006-01-02")
			c.CompletedAt = &date
		}

		// initialize empty slice to avoid null
		c.Comments = []CommentResponse{}

		idToIndex[c.ID] = len(clients)
		clientIDs = append(clientIDs, c.ID)
		clients = append(clients, c)
	}

	// ------------------------------------------------------------
	// Batch load comments (fixes N+1 problem)
	// ------------------------------------------------------------
	if len(clientIDs) > 0 {
		commentRows, err := h.DB.QueryContext(ctx, `
			SELECT id, entity_id, author, body, metadata, created_at, updated_at
			FROM comments
			WHERE entity_type = 'client'
			  AND entity_id = ANY($1)
			ORDER BY created_at DESC
		`, pq.Array(clientIDs))

		if err == nil {
			defer commentRows.Close()

			for commentRows.Next() {
				var id int
				var entityID int64
				var author sql.NullString
				var body string
				var metadata sql.NullString
				var created, updated time.Time

				if err := commentRows.Scan(&id, &entityID, &author, &body, &metadata, &created, &updated); err != nil {
					continue
				}

				var meta map[string]interface{}
				if metadata.Valid && metadata.String != "" {
					_ = json.Unmarshal([]byte(metadata.String), &meta)
				}

				var a *string
				if author.Valid {
					s := author.String
					a = &s
				}

				if idx, ok := idToIndex[entityID]; ok {
					clients[idx].Comments = append(clients[idx].Comments, CommentResponse{
						ID:         id,
						EntityType: "client",
						EntityID:   int(entityID),
						Author:     a,
						Body:       body,
						Metadata:   meta,
						CreatedAt:  created.Format(time.RFC3339),
						UpdatedAt:  updated.Format(time.RFC3339),
					})
				}
			}
		}
	}

	if clients == nil {
		clients = []ClientResponse{}
	}

	activeCount := 0
	for _, c := range clients {
		if c.Status == "active" {
			activeCount++
		}
	}

	log.Printf("ListClients: returning %d clients (active=%d)", len(clients), activeCount)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(clients)
}

// DEBUG: GET /api/debug/active-clients
// Returns names and end dates of all active clients for reconciliation with import file
func (h *Handler) DebugActiveClients(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	type ActiveClientDebug struct {
		Name            string  `json:"name"`
		Email           *string `json:"email,omitempty"`
		ContractEndDate *string `json:"contract_end_date,omitempty"`
	}

	rows, err := h.DB.QueryContext(ctx, `
SELECT 
	c.name,
	c.email,
	MAX(ct.end_date)::text AS end_date
FROM clients c
LEFT JOIN contracts ct ON ct.client_id = c.id
	AND (ct.end_date IS NULL OR ct.end_date >= CURRENT_DATE)
GROUP BY c.id, c.name, c.email
HAVING EXISTS (
	SELECT 1
	FROM contracts ct2
	WHERE ct2.client_id = c.id
	  AND (ct2.end_date IS NULL OR ct2.end_date >= CURRENT_DATE)
)
ORDER BY c.name ASC
`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var activeClients []ActiveClientDebug
	for rows.Next() {
		var client ActiveClientDebug
		var email sql.NullString
		var endDate sql.NullString
		if err := rows.Scan(&client.Name, &email, &endDate); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if email.Valid {
			client.Email = &email.String
		}
		if endDate.Valid {
			client.ContractEndDate = &endDate.String
		}
		activeClients = append(activeClients, client)
	}

	log.Printf("DebugActiveClients: found %d active clients", len(activeClients))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"count":   len(activeClients),
		"clients": activeClients,
	})
}

// DEBUG: GET /api/debug/expired-but-active
// Returns clients with status='active' but all contracts expired (end_date < today)
func (h *Handler) DebugExpiredButActive(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	type ExpiredClientDebug struct {
		Name              string  `json:"name"`
		Email             *string `json:"email,omitempty"`
		LatestContractEnd *string `json:"latest_contract_end,omitempty"`
	}

	rows, err := h.DB.QueryContext(ctx, `
SELECT 
	c.id,
	c.name,
	c.email,
	MAX(ct.end_date)::text AS latest_end_date
FROM clients c
LEFT JOIN contracts ct ON ct.client_id = c.id
WHERE c.status = 'active'
GROUP BY c.id, c.name, c.email
HAVING (
	SELECT COUNT(1) FROM contracts ct2
	WHERE ct2.client_id = c.id
	  AND (ct2.end_date IS NULL OR ct2.end_date >= CURRENT_DATE)
) = 0
ORDER BY c.name ASC
`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var expiredClients []ExpiredClientDebug
	for rows.Next() {
		var id int
		var client ExpiredClientDebug
		var email sql.NullString
		var latestEnd sql.NullString
		if err := rows.Scan(&id, &client.Name, &email, &latestEnd); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if email.Valid {
			client.Email = &email.String
		}
		if latestEnd.Valid {
			client.LatestContractEnd = &latestEnd.String
		}
		expiredClients = append(expiredClients, client)
	}

	log.Printf("DebugExpiredButActive: found %d expired-but-active clients", len(expiredClients))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"count":   len(expiredClients),
		"clients": expiredClients,
	})
}

// DEBUG: GET /api/debug/no-contracts
// Returns clients with no contracts at all
func (h *Handler) DebugNoContracts(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	type NoContractClientDebug struct {
		ID     int     `json:"id"`
		Name   string  `json:"name"`
		Email  *string `json:"email,omitempty"`
		Status string  `json:"status"`
	}

	rows, err := h.DB.QueryContext(ctx, `
SELECT 
	c.id,
	c.name,
	c.email,
	c.status
FROM clients c
WHERE NOT EXISTS (
	SELECT 1 FROM contracts ct WHERE ct.client_id = c.id
)
ORDER BY c.name ASC
`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var noContractClients []NoContractClientDebug
	for rows.Next() {
		var client NoContractClientDebug
		var email sql.NullString
		if err := rows.Scan(&client.ID, &client.Name, &email, &client.Status); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if email.Valid {
			client.Email = &email.String
		}
		noContractClients = append(noContractClients, client)
	}

	log.Printf("DebugNoContracts: found %d clients with no contracts", len(noContractClients))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"count":   len(noContractClients),
		"clients": noContractClients,
	})
}

// POST /api/clients
func (h *Handler) CreateClient(w http.ResponseWriter, r *http.Request) {
	var c Client
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		http.Error(w, "Ungültige Anfrage", http.StatusBadRequest)
		return
	}

	if c.Status == "" {
		http.Error(w, "status is required", http.StatusBadRequest)
		return
	}

	err := h.DB.QueryRow(
		`INSERT INTO clients (name, email, phone, source, source_stage_id, status)
         VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		c.Name, c.Email, c.Phone, c.Source, c.SourceStageID, c.Status,
	).Scan(&c.ID)

	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) {
			log.Printf("Postgres error: %s (code=%s, constraint=%s)", pqErr.Message, pqErr.Code, pqErr.Constraint)

			if pqErr.Code == "23505" {
				if pqErr.Constraint == "unique_client_email" {
					writeJSONError(w, "Ein Kunde mit dieser E-Mail-Adresse existiert bereits.", http.StatusConflict)
					return
				}
				writeJSONError(w, "Doppelter Eintrag: "+pqErr.Constraint, http.StatusConflict)
				return
			}
		}

		writeJSONError(w, "Fehler beim Anlegen des Kunden.", http.StatusInternalServerError)
		return
	}

	if len(c.Comments) > 0 {
		if err := h.insertCommentsForEntity("client", c.ID, c.Comments); err != nil {
			log.Printf("failed to insert comments for client %d: %v", c.ID, err)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(c)
}

// DELETE /api/clients/{id}
func (h *Handler) DeleteClient(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDFromURL(r.URL.Path)
	if !ok {
		http.Error(w, "invalid client ID", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		http.Error(w, "failed to start delete transaction", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		UPDATE leads
		SET converted = FALSE,
		    converted_at = NULL,
		    converted_client_id = NULL
		WHERE converted_client_id = $1
	`, id); err != nil {
		http.Error(w, "failed to reset linked leads: "+err.Error(), http.StatusInternalServerError)
		return
	}

	result, err := tx.ExecContext(ctx, `DELETE FROM clients WHERE id = $1`, id)
	if err != nil {
		http.Error(w, "failed to delete client: "+err.Error(), http.StatusInternalServerError)
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		http.Error(w, "client not found", http.StatusNotFound)
		return
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, "failed to commit delete transaction", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// PATCH /api/clients/{id}
func (h *Handler) UpdateClient(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDFromURL(r.URL.Path)
	if !ok {
		http.Error(w, "invalid client ID", http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil || len(body) == 0 {
		http.Error(w, "empty or unreadable body", http.StatusBadRequest)
		return
	}
	log.Printf("PATCH /clients raw body: %s", string(body))

	// Unmarshal into a generic map for flexible types
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	// Unmarshal into an auxiliary struct so completed_at is a string
	var updated struct {
		Name          string                 `json:"name"`
		Email         string                 `json:"email"`
		Phone         string                 `json:"phone"`
		Source        string                 `json:"source"`
		SourceStageID *int                   `json:"source_stage_id,omitempty"`
		Status        string                 `json:"status"`
		CompletedAt   *string                `json:"completed_at,omitempty"`
		Comments      []CommentCreateRequest `json:"comments,omitempty"`
	}
	if err := json.Unmarshal(body, &updated); err != nil {
		log.Printf("❌ decode error: %v", err)
		http.Error(w, "invalid client data", http.StatusBadRequest)
		return
	}

	// Parse completed_at string (if any)
	var completedAt *time.Time
	if updated.CompletedAt != nil && *updated.CompletedAt != "" {
		if t, err := time.Parse("2006-01-02", *updated.CompletedAt); err == nil {
			completedAt = &t
		} else {
			http.Error(w, "invalid completed_at, expected YYYY-MM-DD", http.StatusBadRequest)
			return
		}
	}

	// Only validate completed_at if the value is actually changing.
	// Frontends often echo back the existing value; re-validating it would incorrectly
	// reject seeded or historically-set dates that pre-date created_at.
	var existingCompletedAt sql.NullTime
	_ = h.DB.QueryRowContext(r.Context(), `SELECT completed_at FROM clients WHERE id = $1`, id).Scan(&existingCompletedAt)
	existingDateStr := ""
	if existingCompletedAt.Valid {
		existingDateStr = existingCompletedAt.Time.Format("2006-01-02")
	}
	incomingDateStr := ""
	if updated.CompletedAt != nil {
		incomingDateStr = *updated.CompletedAt
	}
	if incomingDateStr != existingDateStr {
		if err := h.validateClientCompletedAt(r.Context(), id, completedAt); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	query := `
		UPDATE clients
		SET name = COALESCE($1, name),
			email = COALESCE($2, email),
			phone = COALESCE($3, phone),
			source = COALESCE($4, source),
			source_stage_id = COALESCE($5, source_stage_id),
			status = COALESCE($6, status),
			completed_at = $7
		WHERE id = $8
	`

	_, err = h.DB.Exec(
		query,
		nullStr(updated.Name),
		nullStr(updated.Email),
		nullStr(updated.Phone),
		nullStr(updated.Source),
		nullInt(updated.SourceStageID),
		nullStr(updated.Status),
		nullTime(completedAt),
		id,
	)
	if err != nil {
		log.Printf("❌ update failed: %v", err)
		http.Error(w, "update failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Sync the same contact fields to the linked converted lead (if any).
	// Uses COALESCE so only non-empty values overwrite; blank fields are ignored.
	if _, err := h.DB.Exec(`
		UPDATE leads
		SET
			name            = COALESCE(NULLIF($1,''), name),
			email           = COALESCE(NULLIF($2,''), email),
			phone           = COALESCE(NULLIF($3,''), phone),
			source          = COALESCE(NULLIF($4,''), source),
			source_stage_id = COALESCE($5, source_stage_id)
		WHERE converted_client_id = $6
	`,
		updated.Name,
		updated.Email,
		updated.Phone,
		updated.Source,
		nullInt(updated.SourceStageID),
		id,
	); err != nil {
		log.Printf("❌ lead sync failed for client %d: %v", id, err)
		// non-fatal — client was updated successfully
	}

	// optionally insert comments provided in the patch
	if updated.Comments != nil && len(updated.Comments) > 0 {
		if err := h.insertCommentsForEntity("client", id, updated.Comments); err != nil {
			log.Printf("failed to insert comments for client %d: %v", id, err)
		}
	}

	w.WriteHeader(http.StatusNoContent)
}
