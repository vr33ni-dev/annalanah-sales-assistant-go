package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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

// GET /api/clients
func (h *Handler) ListClients(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	type ClientResponse struct {
		ID              int64             `json:"id"`
		Name            string            `json:"name"`
		Email           string            `json:"email"`
		Phone           string            `json:"phone"`
		Source          string            `json:"source"`
		SourceStageName string            `json:"source_stage_name"`
		Status          string            `json:"status"`
		CompletedAt     *string           `json:"completed_at,omitempty"`
		Comments        []CommentResponse `json:"comments,omitempty"`
	}

	rows, err := h.DB.QueryContext(ctx, `
SELECT 
  c.id,
  c.name,
  c.email,
  c.phone,
  c.source,
  COALESCE(s.name, '') AS source_stage_name,
  COALESCE(c.status, 'new') AS status,
  c.completed_at
FROM clients c
LEFT JOIN stages s ON s.id = c.source_stage_id
ORDER BY c.id
`)
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
		var completedAt sql.NullTime
		var emailNS, phoneNS, sourceNS sql.NullString

		if err := rows.Scan(
			&c.ID, &c.Name, &emailNS,
			&phoneNS, &sourceNS, &c.SourceStageName, &c.Status, &completedAt,
		); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
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
	// 🔥 Batch load comments (fixes N+1 problem)
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

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(clients)
}

// POST /api/clients
// (UNCHANGED — exactly your original code)
func (h *Handler) CreateClient(w http.ResponseWriter, r *http.Request) {
	var c Client
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		http.Error(w, "Ungültige Anfrage", http.StatusBadRequest)
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

	result, err := h.DB.ExecContext(ctx, `DELETE FROM clients WHERE id = $1`, id)
	if err != nil {
		http.Error(w, "failed to delete client: "+err.Error(), http.StatusInternalServerError)
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		http.Error(w, "client not found", http.StatusNotFound)
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
			log.Printf("⚠️ could not parse completed_at: %v", err)
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

	// optionally insert comments provided in the patch
	if updated.Comments != nil && len(updated.Comments) > 0 {
		if err := h.insertCommentsForEntity("client", id, updated.Comments); err != nil {
			log.Printf("failed to insert comments for client %d: %v", id, err)
		}
	}

	w.WriteHeader(http.StatusNoContent)
}
