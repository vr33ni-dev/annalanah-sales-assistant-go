package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/lib/pq"
)

type SalesProcess struct {
	ID                 int      `json:"id"`
	ClientID           int      `json:"client_id"`
	Stage              string   `json:"stage"`
	InitialContactDate *string  `json:"initial_contact_date"`
	FollowUpDate       *string  `json:"follow_up_date"`
	FollowUpResult     *bool    `json:"follow_up_result"`
	Closed             *bool    `json:"closed"`
	Revenue            *float64 `json:"revenue"`
	StageID            *int     `json:"stage_id"`
	LeadID             *int     `json:"lead_id,omitempty"`
}

// What the API returns (GET /api/sales, PATCH /api/sales/{id})
type SalesProcessResponse struct {
	ID                 int               `json:"id"`
	ClientID           int               `json:"client_id"`
	ClientName         string            `json:"client_name"`
	ClientEmail        *string           `json:"client_email,omitempty"`
	ClientPhone        *string           `json:"client_phone,omitempty"`
	ClientSource       *string           `json:"client_source,omitempty"`
	Stage              string            `json:"stage"`
	CreatedAt          *string           `json:"created_at,omitempty"`
	UpdatedAt          *string           `json:"updated_at,omitempty"`
	InitialContactDate *string           `json:"initial_contact_date"`
	FollowUpDate       *string           `json:"follow_up_date"`
	FollowUpResult     *bool             `json:"follow_up_result"`
	Closed             *bool             `json:"closed"`
	Revenue            *float64          `json:"revenue"`
	StageID            *int              `json:"stage_id"`
	LeadID             *int              `json:"lead_id,omitempty"`
	Comments           []CommentResponse `json:"comments,omitempty"`
}

// What the API accepts (PATCH /api/sales/{id})
type SalesProcessUpdateRequest struct {
	InitialContactDate     *string                `json:"initial_contact_date,omitempty"`
	FollowUpDate           *string                `json:"follow_up_date,omitempty"`
	FollowUpResult         *bool                  `json:"follow_up_result"`
	Closed                 *bool                  `json:"closed"`
	Revenue                *float64               `json:"revenue"`
	ContractDurationMonths *int                   `json:"contract_duration_months,omitempty"`
	ContractStartDate      *string                `json:"contract_start_date,omitempty"`
	ContractFrequency      *string                `json:"contract_frequency,omitempty"`
	CompletedAt            *string                `json:"completed_at,omitempty"`
	Comments               []CommentCreateRequest `json:"comments,omitempty"`
}

// GET /api/sales
func (h *Handler) ListSalesProcesses(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.Query(`
	SELECT
		sp.id,
		sp.client_id,
		cl.name  AS client_name,
		cl.email AS client_email,
		cl.phone AS client_phone,
		cl.source AS client_source,
		sp.stage,
		sp.created_at,
		sp.initial_contact_date,
		sp.follow_up_date,
		sp.follow_up_result,
		sp.closed,
		CASE WHEN COALESCE(sp.closed, false) THEN sp.revenue ELSE NULL END AS revenue,
		sp.stage_id,
		sp.lead_id
	FROM sales_process sp
	JOIN clients cl ON cl.id = sp.client_id
	ORDER BY sp.created_at DESC, sp.id DESC
`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var processes []SalesProcessResponse
	var salesIDs []int
	idToIndex := make(map[int]int)

	for rows.Next() {
		var sp SalesProcessResponse
		if err := rows.Scan(
			&sp.ID,
			&sp.ClientID,
			&sp.ClientName,
			&sp.ClientEmail,
			&sp.ClientPhone,
			&sp.ClientSource,
			&sp.Stage,
			&sp.CreatedAt,
			&sp.InitialContactDate,
			&sp.FollowUpDate,
			&sp.FollowUpResult,
			&sp.Closed,
			&sp.Revenue,
			&sp.StageID,
			&sp.LeadID,
		); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		sp.Comments = []CommentResponse{}

		idToIndex[sp.ID] = len(processes)
		salesIDs = append(salesIDs, sp.ID)
		processes = append(processes, sp)
	}

	// Batch load sales_process comments (avoid N+1)
	if len(salesIDs) > 0 {
		commentRows, err := h.DB.Query(`
			SELECT id, entity_id, author, body, metadata, created_at, updated_at
			FROM comments
			WHERE entity_type = 'sales_process'
			  AND entity_id = ANY($1)
			ORDER BY created_at DESC
		`, pq.Array(salesIDs))

		if err == nil {
			defer commentRows.Close()

			for commentRows.Next() {
				var id int
				var entityID int
				var author sql.NullString
				var body string
				var metadata sql.NullString
				var created, updated time.Time

				if err := commentRows.Scan(
					&id,
					&entityID,
					&author,
					&body,
					&metadata,
					&created,
					&updated,
				); err != nil {
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
					processes[idx].Comments = append(processes[idx].Comments, CommentResponse{
						ID:         id,
						EntityType: "sales_process",
						EntityID:   entityID,
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

	if processes == nil {
		processes = []SalesProcessResponse{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(processes)
}

// PATCH /api/sales/{id}
func (h *Handler) UpdateSalesProcess(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid sales process id", http.StatusBadRequest)
		return
	}

	// Use the update request type that can carry contract details
	var sp SalesProcessUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&sp); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// ---------- VALIDATION ----------
	// If closed=true, all contract fields must be present/valid.
	if sp.Closed != nil && *sp.Closed == true {
		if sp.Revenue == nil ||
			sp.ContractDurationMonths == nil || *sp.ContractDurationMonths <= 0 ||
			sp.ContractStartDate == nil ||
			sp.ContractFrequency == nil ||
			(*sp.ContractFrequency != "monthly" && *sp.ContractFrequency != "bi-monthly" && *sp.ContractFrequency != "quarterly" && *sp.ContractFrequency != "one-time" && *sp.ContractFrequency != "bi-yearly") {
			http.Error(w, "cannot set closed=true without contract details (revenue, duration>0, start date, frequency)", http.StatusBadRequest)
			return
		}
	}

	// Small ergonomics: if closed=true but result wasn’t provided, assume the call happened
	if sp.Closed != nil && *sp.Closed == true && sp.FollowUpResult == nil {
		t := true
		sp.FollowUpResult = &t
	}

	// ---------- UPDATE SALES_PROCESS (fields + normalized stage) ----------
	_, err = h.DB.Exec(`
	UPDATE sales_process
	SET
		initial_contact_date = COALESCE($1, initial_contact_date),
		follow_up_date       = COALESCE($2, follow_up_date),
		follow_up_result     = COALESCE($3, follow_up_result),
		closed               = COALESCE($4, closed),
		revenue              = CASE
			WHEN $4 IS TRUE  THEN $5
			WHEN $4 IS FALSE THEN NULL
			ELSE revenue
		END,
		stage = CASE
			WHEN COALESCE($4, closed) IS TRUE  THEN 'closed'
			WHEN $4 IS NOT NULL AND $4 IS FALSE THEN 'lost'
			WHEN stage = 'lost' AND $4 IS NULL AND $3 IS NULL THEN 'lost'
			WHEN COALESCE($2, follow_up_date) IS NOT NULL THEN 'follow_up'
			WHEN COALESCE($1, initial_contact_date) IS NOT NULL THEN 'initial_contact'
			WHEN COALESCE($3, follow_up_result) IS FALSE THEN 'lost'
			WHEN COALESCE($3, follow_up_result) IS TRUE  THEN 'follow_up'
			ELSE 'follow_up'
		END
	WHERE id = $6
`,
		sp.InitialContactDate,
		sp.FollowUpDate,
		sp.FollowUpResult,
		sp.Closed,
		sp.Revenue,
		id,
	)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// ---------- SYNC CLIENT STATUS ----------
	_, err = h.DB.Exec(`
	WITH s AS (
		SELECT client_id, stage, follow_up_result, closed, initial_contact_date
		FROM sales_process WHERE id = $1
	)
	  UPDATE clients c
	  SET status = CASE
	    WHEN (SELECT stage FROM s) = 'closed'
	         AND COALESCE((SELECT closed FROM s), FALSE) = TRUE
	      THEN 'active'
	    WHEN (SELECT stage FROM s) = 'lost'
	      THEN 'lost'
		WHEN (SELECT stage FROM s) = 'initial_contact'
			 AND (SELECT initial_contact_date FROM s) IS NOT NULL
			 AND (SELECT follow_up_result FROM s) IS NULL
		THEN 'initial_call_scheduled'
		WHEN (SELECT stage FROM s) = 'follow_up'
				 AND (SELECT follow_up_result FROM s) IS NULL
			THEN 'follow_up_scheduled'
	    WHEN (SELECT stage FROM s) = 'follow_up'
	         AND (SELECT follow_up_result FROM s) IS TRUE
	      THEN 'awaiting_response'
	    ELSE c.status
	  END
	  WHERE c.id = (SELECT client_id FROM s)
	`, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// ---------- (OPTIONAL) AUTO-CREATE CONTRACT ON CLOSE-WON ----------
	if sp.Closed != nil && *sp.Closed == true &&
		sp.Revenue != nil &&
		sp.ContractDurationMonths != nil && *sp.ContractDurationMonths > 0 &&
		sp.ContractStartDate != nil && sp.ContractFrequency != nil {

		// get client_id for this sales process
		var clientID int
		if err := h.DB.QueryRow(`SELECT client_id FROM sales_process WHERE id = $1`, id).Scan(&clientID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// If Closed is explicitly true, update clients.completed_at (set to provided date)
		// If closed is explicitly false, clear the client's completed_at.
		if sp.Closed != nil && *sp.Closed && sp.CompletedAt != nil {
			_, err := h.DB.Exec(`
		UPDATE clients
		SET completed_at = $1::date
		WHERE id = $2
		`, *sp.CompletedAt, clientID)
			if err != nil {
				http.Error(w, "failed to update completed_at: "+err.Error(), http.StatusInternalServerError)
				return
			}
		} else if sp.Closed != nil && !*sp.Closed {
			_, err := h.DB.Exec(`UPDATE clients SET completed_at = NULL WHERE id = $1`, clientID)
			if err != nil {
				http.Error(w, "failed to clear completed_at: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}

		tx2, err := h.DB.BeginTx(r.Context(), nil)
		if err != nil {
			http.Error(w, "failed to begin tx: "+err.Error(), http.StatusInternalServerError)
			return
		}
		defer tx2.Rollback()

		// insert contract
		var newContractID int
		err = tx2.QueryRowContext(r.Context(), `
		INSERT INTO contracts (
			client_id, sales_process_id, start_date,
			duration_months, revenue_total, payment_frequency
		)
		VALUES ($1, $2, $3::date, $4, $5, $6)
		RETURNING id
	`,
			clientID,
			id,
			*sp.ContractStartDate,
			*sp.ContractDurationMonths,
			*sp.Revenue,
			*sp.ContractFrequency,
		).Scan(&newContractID)

		if err != nil {
			http.Error(w, "failed to create contract: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// insert the full schedule for the new contract (atomic within tx)
		sd, err := time.Parse("2006-01-02", *sp.ContractStartDate)
		if err != nil {
			http.Error(w, "invalid contract_start_date", http.StatusBadRequest)
			return
		}
		endDate := sd.AddDate(0, *sp.ContractDurationMonths, 0)

		if err := insertCashflowEntriesTx(
			tx2,
			newContractID,
			sd,
			endDate,
			*sp.Revenue,
			*sp.ContractFrequency,
		); err != nil {
			http.Error(w, "failed to create cashflow entries: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// ---------- CONVERT LEAD → CLIENT (ONLY ON CONTRACT) ----------
		if _, err = tx2.ExecContext(r.Context(), `
	UPDATE leads
	SET
		converted = TRUE,
		converted_at = now(),
		converted_client_id = $1
	WHERE id = (
		SELECT lead_id
		FROM sales_process
		WHERE id = $2
		  AND lead_id IS NOT NULL
	)
	  AND converted = FALSE
`, clientID, id); err != nil {
			http.Error(w, "failed to convert lead: "+err.Error(), http.StatusInternalServerError)
			return
		}

		if err := tx2.Commit(); err != nil {
			http.Error(w, "commit failed: "+err.Error(), http.StatusInternalServerError)
			return
		}

	}

	// ---------- RETURN UPDATED ROW ----------
	row := h.DB.QueryRow(`
	  SELECT
	    sp.id,
	    sp.client_id,
	    c.name  AS client_name,
	    c.email AS client_email,
	    c.phone AS client_phone,
	    c.source AS client_source,
	    sp.stage,
			sp.initial_contact_date,
	    sp.follow_up_date,
	    sp.follow_up_result,
	    sp.closed,
	    CASE WHEN COALESCE(sp.closed, false) THEN sp.revenue ELSE NULL END AS revenue,
	    sp.stage_id
	  FROM sales_process sp
	  JOIN clients c ON c.id = sp.client_id
	  WHERE sp.id = $1
	`, id)

	var updated SalesProcessResponse
	if err := row.Scan(
		&updated.ID,
		&updated.ClientID,
		&updated.ClientName,
		&updated.ClientEmail,
		&updated.ClientPhone,
		&updated.ClientSource,
		&updated.Stage,
		&updated.InitialContactDate,
		&updated.FollowUpDate,
		&updated.FollowUpResult,
		&updated.Closed,
		&updated.Revenue,
		&updated.StageID,
	); err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "sales process not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// load comments for this sales process
	commentRows, err := h.DB.Query(`
		SELECT id, author, body, metadata, created_at, updated_at
		FROM comments
		WHERE entity_type = 'sales_process' AND entity_id = $1
		ORDER BY created_at DESC
	`, updated.ID)
	if err == nil {
		var comments []CommentResponse
		for commentRows.Next() {
			var id int
			var author sql.NullString
			var body string
			var metadata sql.NullString
			var created, updatedAt time.Time
			if err := commentRows.Scan(&id, &author, &body, &metadata, &created, &updatedAt); err == nil {
				var meta map[string]interface{}
				if metadata.Valid && metadata.String != "" {
					_ = json.Unmarshal([]byte(metadata.String), &meta)
				}
				var a *string
				if author.Valid {
					s := author.String
					a = &s
				}
				comments = append(comments, CommentResponse{
					ID: id, EntityType: "sales_process", EntityID: updated.ID, Author: a, Body: body, Metadata: meta,
					CreatedAt: created.Format(time.RFC3339), UpdatedAt: updatedAt.Format(time.RFC3339),
				})
			}
		}
		_ = commentRows.Close()
		if comments == nil {
			comments = []CommentResponse{}
		}
		updated.Comments = comments
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(updated)
}

// POST /api/sales/start
type StartSalesProcessRequest struct {
	Name               string                 `json:"name"`
	Email              string                 `json:"email"`
	Phone              string                 `json:"phone"`
	Source             string                 `json:"source"`
	SourceStageID      *int                   `json:"source_stage_id,omitempty"`
	InitialContactDate *string                `json:"initial_contact_date,omitempty"`
	FollowUpDate       *string                `json:"follow_up_date"`
	LeadID             *int                   `json:"lead_id,omitempty"`
	MergeStrategy      *string                `json:"merge_strategy,omitempty"` // overwrite | keep_existing
	ClientID           *int                   `json:"client_id,omitempty"`
	Comments           []CommentCreateRequest `json:"comments,omitempty"`
}

// ClientResponse is the nested client object returned inside StartSalesProcessResponse.
// It intentionally contains a compact subset of client fields and any comments.
type ClientResponse struct {
	ID            int               `json:"id"`
	Name          string            `json:"name"`
	Email         string            `json:"email"`
	Phone         string            `json:"phone"`
	Source        string            `json:"source"`
	SourceStageID *int              `json:"source_stage_id,omitempty"`
	Comments      []CommentResponse `json:"comments,omitempty"`
}

// SalesProcessSummary is the nested sales-process object returned inside StartSalesProcessResponse.
// It represents a compact summary of the newly created sales process.
type SalesProcessSummary struct {
	ID                 int     `json:"id"`
	ClientID           int     `json:"client_id"`
	Stage              string  `json:"stage"`
	InitialContactDate *string `json:"initial_contact_date"`
	FollowUpDate       *string `json:"follow_up_date"`
	FollowUpResult     *bool   `json:"follow_up_result"`
	Closed             *bool   `json:"closed"`
	Revenue            *int    `json:"revenue"`
	StageID            *int    `json:"stage_id"`
	LeadID             *int    `json:"lead_id,omitempty"`
}

type StartSalesProcessResponse struct {
	SalesProcessID int                 `json:"sales_process_id"`
	Client         ClientResponse      `json:"client"`
	SalesProcess   SalesProcessSummary `json:"sales_process"`
}

func (h *Handler) StartSalesProcess(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// ------------------------------------------------
	// 0) Parse & validate request
	// ------------------------------------------------
	var req StartSalesProcessRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.InitialContactDate == nil || strings.TrimSpace(*req.InitialContactDate) == "" {
		http.Error(w, "initial_contact_date is required", http.StatusBadRequest)
		return
	}

	normalize := func(s string) string {
		return strings.ToLower(strings.TrimSpace(s))
	}

	// ------------------------------------------------
	// 1) Resolve lead (ignore converted)
	// ------------------------------------------------
	var foundLeadID *int

	if req.LeadID != nil {
		var converted bool
		if err := h.DB.QueryRowContext(ctx,
			`SELECT converted FROM leads WHERE id = $1`,
			*req.LeadID,
		).Scan(&converted); err == nil && !converted {
			foundLeadID = req.LeadID
		}
	}

	if foundLeadID == nil && strings.TrimSpace(req.Email) != "" {
		var id int
		if err := h.DB.QueryRowContext(ctx, `
			SELECT id FROM leads
			WHERE LOWER(email) = LOWER($1)
			  AND converted = FALSE
			ORDER BY id DESC
			LIMIT 1
		`, req.Email).Scan(&id); err == nil {
			foundLeadID = &id
		}
	}

	// If we found a lead and the caller didn't provide source_stage_id or source,
	// copy them from the lead so created client/sales_process inherit the attribution.
	if foundLeadID != nil {
		var leadSource sql.NullString
		var leadSourceStage sql.NullInt64
		if err := h.DB.QueryRowContext(ctx, `SELECT source, source_stage_id FROM leads WHERE id = $1`, *foundLeadID).Scan(&leadSource, &leadSourceStage); err == nil {
			if req.SourceStageID == nil && leadSourceStage.Valid {
				v := int(leadSourceStage.Int64)
				req.SourceStageID = &v
			}
			if strings.TrimSpace(req.Source) == "" && leadSource.Valid {
				req.Source = leadSource.String
			}
		}
	}

	// ------------------------------------------------
	// 2) Resolve existing client (PIN via client_id if present)
	// ------------------------------------------------
	var existingClientID *int
	var existing struct {
		ID     int
		Name   string
		Phone  sql.NullString
		Source sql.NullString
	}

	if req.ClientID != nil {
		existingClientID = req.ClientID
		_ = h.DB.QueryRowContext(ctx,
			`SELECT name, phone, source FROM clients WHERE id = $1`,
			*req.ClientID,
		).Scan(&existing.Name, &existing.Phone, &existing.Source)
		existing.ID = *req.ClientID
	}

	// ------------------------------------------------
	// 3) ABSOLUTE HARD STOP: active contract
	// ------------------------------------------------
	if existingClientID != nil {
		var hasActiveContract bool
		err := h.DB.QueryRowContext(ctx, `
    SELECT EXISTS (
        SELECT 1
        FROM contracts
        WHERE client_id = $1
          AND end_date >= CURRENT_DATE
    )
`, *existingClientID).Scan(&hasActiveContract)

		if err != nil {
			http.Error(w, "contract lookup failed: "+err.Error(), http.StatusInternalServerError)
			return
		}

		if hasActiveContract {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":     "client_has_active_contract",
				"client_id": *existingClientID,
			})
			return // ⛔ ABSOLUTE STOP
		}

	}

	// ------------------------------------------------
	// 4) Detect conflicts
	// ------------------------------------------------
	conflicts := map[string]any{}

	if existingClientID != nil {
		if normalize(req.Name) != normalize(existing.Name) {
			conflicts["name"] = map[string]any{
				"existing": existing.Name,
				"incoming": req.Name,
			}
		}

		if req.Phone != "" && existing.Phone.Valid &&
			normalize(req.Phone) != normalize(existing.Phone.String) {
			conflicts["phone"] = map[string]any{
				"existing": existing.Phone.String,
				"incoming": req.Phone,
			}
		}

		if req.Source != "" && existing.Source.Valid &&
			normalize(req.Source) != normalize(existing.Source.String) {
			conflicts["source"] = map[string]any{
				"existing": existing.Source.String,
				"incoming": req.Source,
			}
		}
	}

	// ------------------------------------------------
	// 5) Merge decision required
	// ------------------------------------------------
	// If the caller selected an existing lead, prefer allowing the incoming
	// non-empty fields to update the existing client (merge/overwrite)
	// so the UI can adjust e.g. `source` while reusing the client.
	if existingClientID != nil && foundLeadID != nil && req.MergeStrategy == nil {
		ov := "overwrite"
		req.MergeStrategy = &ov
	}
	if existingClientID != nil &&
		len(conflicts) > 0 &&
		req.MergeStrategy == nil {

		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":               "client_exists",
			"client_id":           *existingClientID,
			"has_active_contract": false,
			"conflicts":           conflicts,
			"original_payload":    req,
		})
		return
	}

	// ------------------------------------------------
	// 6) Transaction (WRITES ONLY)
	// ------------------------------------------------
	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		http.Error(w, "tx begin failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	// ------------------------------------------------
	// 7) Create or update client
	// ------------------------------------------------
	var clientID int

	if existingClientID != nil {
		clientID = *existingClientID

		if req.MergeStrategy != nil && *req.MergeStrategy == "overwrite" {
			if _, err := tx.ExecContext(ctx, `
				UPDATE clients
				SET
					name   = COALESCE(NULLIF($1,''), name),
					phone  = COALESCE(NULLIF($2,''), phone),
					source = COALESCE(NULLIF($3,''), source)
				WHERE id = $4
			`, req.Name, req.Phone, req.Source, clientID); err != nil {
				http.Error(w, "client overwrite failed: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
	} else {
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO clients (name, email, phone, source, source_stage_id)
			VALUES ($1,$2,$3,$4,$5)
			RETURNING id
		`,
			req.Name, req.Email, req.Phone, req.Source, req.SourceStageID,
		).Scan(&clientID); err != nil {
			http.Error(w, "client insert failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// ------------------------------------------------
	// 7.1) Overwrite lead data (ONLY if overwrite chosen)
	// ------------------------------------------------
	if req.MergeStrategy != nil &&
		*req.MergeStrategy == "overwrite" &&
		foundLeadID != nil {

		if _, err := tx.ExecContext(ctx, `
        UPDATE leads
        SET
            name   = COALESCE(NULLIF($1,''), name),
            email  = COALESCE(NULLIF($2,''), email),
            phone  = COALESCE(NULLIF($3,''), phone),
            source = COALESCE(NULLIF($4,''), source),
            source_stage_id = COALESCE($5, source_stage_id)
        WHERE id = $6
          AND converted = FALSE
    `,
			req.Name,
			req.Email,
			req.Phone,
			req.Source,
			req.SourceStageID,
			*foundLeadID,
		); err != nil {
			http.Error(w, "lead overwrite failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// ------------------------------------------------
	// 8) Create or reuse sales_process
	// ------------------------------------------------
	var salesID int
	// determine stage in Go to avoid ambiguous SQL parameter typing
	stage := "follow_up"
	if req.FollowUpDate != nil && strings.TrimSpace(*req.FollowUpDate) != "" {
		stage = "follow_up"
	} else if req.InitialContactDate != nil && strings.TrimSpace(*req.InitialContactDate) != "" {
		stage = "initial_contact"
	}

	err = tx.QueryRowContext(ctx, `
		INSERT INTO sales_process
			(client_id, initial_contact_date, follow_up_date, stage, stage_id, created_at, lead_id)
		VALUES ($1,$2,$3,$4,$5,now(),$6)
		ON CONFLICT (client_id) DO NOTHING
		RETURNING id
	`,
		clientID,
		req.InitialContactDate,
		req.FollowUpDate,
		stage,
		req.SourceStageID,
		foundLeadID,
	).Scan(&salesID)

	if err == sql.ErrNoRows {
		if err := tx.QueryRowContext(ctx,
			`SELECT id FROM sales_process WHERE client_id = $1`,
			clientID,
		).Scan(&salesID); err != nil {
			http.Error(w, "sales_process reuse failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
	} else if err != nil {
		http.Error(w, "sales_process insert failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, "commit failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// optionally insert comments provided with the create request (post-commit)
	if len(req.Comments) > 0 {
		if err := h.insertCommentsForEntity("client", clientID, req.Comments); err != nil {
			// ignore insertion error for now
		}
	}

	// load comments for response
	var respComments []CommentResponse
	commentRows, err := h.DB.Query(`
		SELECT id, author, body, metadata, created_at, updated_at
		FROM comments
		WHERE entity_type = 'client' AND entity_id = $1
		ORDER BY created_at DESC
	`, clientID)
	if err == nil {
		for commentRows.Next() {
			var id int
			var author sql.NullString
			var body string
			var metadata sql.NullString
			var created, updated time.Time
			if err := commentRows.Scan(&id, &author, &body, &metadata, &created, &updated); err == nil {
				var meta map[string]interface{}
				if metadata.Valid && metadata.String != "" {
					_ = json.Unmarshal([]byte(metadata.String), &meta)
				}
				var a *string
				if author.Valid {
					s := author.String
					a = &s
				}
				respComments = append(respComments, CommentResponse{
					ID: id, EntityType: "client", EntityID: clientID, Author: a, Body: body, Metadata: meta,
					CreatedAt: created.Format(time.RFC3339), UpdatedAt: updated.Format(time.RFC3339),
				})
			}
		}
		_ = commentRows.Close()
		if respComments == nil {
			respComments = []CommentResponse{}
		}
	}

	// ------------------------------------------------
	// 9) Response
	// ------------------------------------------------
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(StartSalesProcessResponse{
		SalesProcessID: salesID,
		Client: ClientResponse{
			ID:       clientID,
			Name:     req.Name,
			Email:    req.Email,
			Phone:    req.Phone,
			Source:   req.Source,
			Comments: respComments,
		},
		SalesProcess: SalesProcessSummary{
			ID:                 salesID,
			ClientID:           clientID,
			Stage:              "follow_up",
			InitialContactDate: req.InitialContactDate,
			FollowUpDate:       req.FollowUpDate,
			StageID:            req.SourceStageID,
			LeadID:             foundLeadID,
		},
	})

}

// createClientAndSalesProcessTx creates a client and a follow-up sales_process inside the provided tx.
// Returns clientID, salesProcessID or error. leadID may be nil.
func (h *Handler) createClientAndSalesProcessTx(
	ctx context.Context,
	tx *sql.Tx,
	name string,
	email *string,
	phone *string,
	source string,
	sourceStageID *int,
	initialContactDate *string,
	followUpDate *string,
	leadID *int,
) (int, int, error) {

	var clientID int
	var salesProcessID int

	// ------------------------------------------
	// 1) Create OR reuse client (email-idempotent)
	// ------------------------------------------
	if email != nil && *email != "" {
		err := tx.QueryRowContext(ctx,
			`SELECT id FROM clients WHERE LOWER(email) = LOWER($1)`,
			*email,
		).Scan(&clientID)

		if err == sql.ErrNoRows {
			err = tx.QueryRowContext(ctx,
				`INSERT INTO clients (name, email, phone, source, source_stage_id)
				 VALUES ($1,$2,$3,$4,$5)
				 RETURNING id`,
				name, email, phone, source, sourceStageID,
			).Scan(&clientID)
			if err != nil {
				return 0, 0, err
			}
		} else if err != nil {
			return 0, 0, err
		}
	} else {
		// no email → always new client
		err := tx.QueryRowContext(ctx,
			`INSERT INTO clients (name, phone, source, source_stage_id)
			 VALUES ($1,$2,$3,$4)
			 RETURNING id`,
			name, phone, source, sourceStageID,
		).Scan(&clientID)
		if err != nil {
			return 0, 0, err
		}
	}

	// ------------------------------------------
	// 2) Create OR reuse sales_process (SAFE)
	// ------------------------------------------

	// determine stage in Go to avoid SQL parameter type ambiguity
	stage := "follow_up"
	if followUpDate != nil && strings.TrimSpace(*followUpDate) != "" {
		stage = "follow_up"
	} else if initialContactDate != nil && strings.TrimSpace(*initialContactDate) != "" {
		stage = "initial_contact"
	}

	err := tx.QueryRowContext(ctx,
		`INSERT INTO sales_process
			(client_id, initial_contact_date, follow_up_date, stage, stage_id, created_at, lead_id)
		 VALUES ($1,$2,$3,$4,$5,now(),$6)
		 ON CONFLICT (client_id)
		 DO NOTHING
		 RETURNING id`,
		clientID,
		initialContactDate,
		followUpDate,
		stage,
		sourceStageID,
		leadID,
	).Scan(&salesProcessID)

	if err == sql.ErrNoRows {
		// conflict happened → reuse existing sales_process
		if err := tx.QueryRowContext(ctx,
			`SELECT id FROM sales_process WHERE client_id = $1`,
			clientID,
		).Scan(&salesProcessID); err != nil {
			return 0, 0, err
		}
	} else if err != nil {
		return 0, 0, err
	}

	return clientID, salesProcessID, nil
}

// Upsells
// Using Pointers because PostgreSQL can return NULL, and Go's plain string cannot represent NULL, only "" and these fields can return nil
type ContractUpsell struct {
	ID                     int        `json:"id"`
	SalesProcessID         int        `json:"sales_process_id"`
	ClientID               int        `json:"client_id"`
	UpsellDate             *string    `json:"upsell_date"`
	UpsellResult           *string    `json:"upsell_result"` // "verlaengerung" or "keine_verlaengerung"`
	UpsellRevenue          *float64   `json:"upsell_revenue,omitempty"`
	ContractStartDate      *time.Time `json:"contract_start_date"`
	ContractDurationMonths *int       `json:"contract_duration_months"`
	ContractFrequency      *string    `json:"contract_frequency"`
	PreviousContractID     *int       `json:"previous_contract_id,omitempty"`
	NewContractID          *int       `json:"new_contract_id,omitempty"`
	CreatedAt              *string    `json:"created_at"`
	UpdatedAt              *string    `json:"updated_at"`
}
type CreateUpsellRequest struct {
	UpsellDate             *string  `json:"upsell_date,omitempty"`
	UpsellResult           *string  `json:"upsell_result,omitempty"`
	UpsellRevenue          *float64 `json:"upsell_revenue,omitempty"`
	ContractStartDate      *string  `json:"contract_start_date,omitempty"`
	ContractDurationMonths *int     `json:"contract_duration_months,omitempty"`
	ContractFrequency      *string  `json:"contract_frequency,omitempty"`
}

func (h *Handler) GetUpsellForSalesProcess(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	salesID, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid sales process id", http.StatusBadRequest)
		return
	}

	rows, err := h.DB.Query(`
SELECT 
    cu.id,
    cu.sales_process_id,
    cu.client_id,
    cu.upsell_date,
    cu.upsell_result,
    cu.upsell_revenue,
    cu.previous_contract_id,
    cu.new_contract_id,
    cu.created_at,
    cu.updated_at,
    c.start_date AS contract_start_date,
    c.duration_months AS contract_duration_months,
    c.payment_frequency AS contract_frequency
FROM contract_upsells cu
LEFT JOIN contracts c
       ON c.id = cu.new_contract_id
WHERE cu.sales_process_id = $1
ORDER BY cu.upsell_date DESC NULLS LAST, cu.id DESC
`, salesID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	var list []ContractUpsell

	for rows.Next() {
		var u ContractUpsell
		if err := rows.Scan(
			&u.ID,
			&u.SalesProcessID,
			&u.ClientID,
			&u.UpsellDate,
			&u.UpsellResult,
			&u.UpsellRevenue,
			&u.PreviousContractID,
			&u.NewContractID,
			&u.CreatedAt,
			&u.UpdatedAt,
			&u.ContractStartDate,
			&u.ContractDurationMonths,
			&u.ContractFrequency,
		); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		list = append(list, u)
	}

	if list == nil {
		list = []ContractUpsell{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(list)
}

func (h *Handler) ListUpsellCategories(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var where []string
	var args []any
	idx := 1

	if v := q.Get("start_date"); v != "" {
		start, err := time.Parse("2006-01-02", v)
		if err != nil {
			http.Error(w, "invalid start_date (expected YYYY-MM-DD)", http.StatusBadRequest)
			return
		}
		where = append(where, "cu.upsell_date >= $"+strconv.Itoa(idx))
		args = append(args, start)
		idx++
	}
	if v := q.Get("end_date"); v != "" {
		end, err := time.Parse("2006-01-02", v)
		if err != nil {
			http.Error(w, "invalid end_date (expected YYYY-MM-DD)", http.StatusBadRequest)
			return
		}
		where = append(where, "cu.upsell_date <= $"+strconv.Itoa(idx))
		args = append(args, end)
		idx++
	}

	whereSQL := ""
	if len(where) > 0 {
		whereSQL = "WHERE " + strings.Join(where, " AND ")
	}

	query := `
SELECT 
    cu.id,
    cu.sales_process_id,
    cu.client_id,
    cu.upsell_date,
    cu.upsell_result,
    cu.upsell_revenue,
    cu.previous_contract_id,
    cu.new_contract_id,
    cu.created_at,
    cu.updated_at,
    c.start_date AS contract_start_date,
    c.duration_months AS contract_duration_months,
    c.payment_frequency AS contract_frequency
FROM contract_upsells cu
LEFT JOIN contracts c
       ON c.id = cu.new_contract_id
` + whereSQL + `
ORDER BY cu.upsell_date DESC NULLS LAST, cu.id DESC
`

	rows, err := h.DB.Query(query, args...)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	var scheduled []ContractUpsell
	var successful []ContractUpsell
	var unsuccessful []ContractUpsell

	for rows.Next() {
		var u ContractUpsell
		if err := rows.Scan(
			&u.ID,
			&u.SalesProcessID,
			&u.ClientID,
			&u.UpsellDate,
			&u.UpsellResult,
			&u.UpsellRevenue,
			&u.PreviousContractID,
			&u.NewContractID,
			&u.CreatedAt,
			&u.UpdatedAt,
			&u.ContractStartDate,
			&u.ContractDurationMonths,
			&u.ContractFrequency,
		); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		if u.UpsellResult == nil {
			scheduled = append(scheduled, u)
		} else if *u.UpsellResult == "verlaengerung" {
			successful = append(successful, u)
		} else if *u.UpsellResult == "keine_verlaengerung" {
			unsuccessful = append(unsuccessful, u)
		}
	}

	resp := map[string]any{
		"scheduled":    scheduled,
		"successful":   successful,
		"unsuccessful": unsuccessful,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *Handler) CreateOrUpdateUpsell(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	salesID, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid sales process id", http.StatusBadRequest)
		return
	}

	var req CreateUpsellRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	// -----------------------
	// VALIDATION
	// -----------------------

	// Validate upsell_result only if provided
	if req.UpsellResult != nil {
		if *req.UpsellResult != "verlaengerung" && *req.UpsellResult != "keine_verlaengerung" {
			http.Error(w, "upsell_result must be 'verlaengerung' or 'keine_verlaengerung'", http.StatusBadRequest)
			return
		}
	}

	// If verlängerung → revenue is required
	if req.UpsellResult != nil && *req.UpsellResult == "verlaengerung" {
		if req.UpsellRevenue == nil {
			http.Error(w, "upsell_revenue required for verlängerung", http.StatusBadRequest)
			return
		}
	}

	// -----------------------
	// Resolve client_id
	// -----------------------

	var clientID int
	err = h.DB.QueryRow(`SELECT client_id FROM sales_process WHERE id = $1`, salesID).Scan(&clientID)
	if err != nil {
		http.Error(w, "sales process not found", http.StatusNotFound)
		return
	}

	tx, err := h.DB.Begin()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer tx.Rollback()

	// -----------------------
	// Check if there is an existing open (pending) upsell
	// pending = has date but no result yet
	// -----------------------

	var existingUpsellID *int
	err = tx.QueryRow(`
			SELECT id FROM contract_upsells
			WHERE sales_process_id = $1
				AND upsell_result IS NULL
			ORDER BY upsell_date DESC NULLS LAST, id DESC
			LIMIT 1
	`, salesID).Scan(&existingUpsellID)

	if err == sql.ErrNoRows {
		existingUpsellID = nil
	} else if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	// -----------------------
	// Determine active previous contract (may be null)
	// -----------------------

	var prevContractID *int
	_ = tx.QueryRow(`
        SELECT id FROM contracts
        WHERE client_id = $1 AND end_date IS NULL
        ORDER BY id DESC LIMIT 1
    `, clientID).Scan(&prevContractID)

	// -----------------------
	// If verlängerung → create new contract + cashflow
	// -----------------------

	var newContractID *int = nil

	if req.UpsellResult != nil && *req.UpsellResult == "verlaengerung" {

		// ----- VALIDATE required contract fields -----
		if req.ContractStartDate == nil ||
			req.ContractDurationMonths == nil || *req.ContractDurationMonths <= 0 ||
			req.ContractFrequency == nil ||
			(*req.ContractFrequency != "monthly" &&
				*req.ContractFrequency != "bi-monthly" &&
				*req.ContractFrequency != "quarterly" &&
				*req.ContractFrequency != "one-time" &&
				*req.ContractFrequency != "bi-yearly") {

			http.Error(w, "contract_start_date, contract_duration_months > 0 and contract_frequency (monthly|bi-monthly|quarterly|one-time|bi-yearly) are required", http.StatusBadRequest)
			return
		}

		// If bi-yearly selected, require duration >= 12 months
		if req.ContractFrequency != nil && *req.ContractFrequency == "bi-yearly" {
			if req.ContractDurationMonths == nil || *req.ContractDurationMonths < 12 {
				http.Error(w, "bi-yearly payment frequency requires contract_duration_months >= 12", http.StatusBadRequest)
				return
			}
		}

		// ----- INSERT CONTRACT -----
		err = tx.QueryRow(`
        INSERT INTO contracts (
            client_id, sales_process_id, start_date,
            duration_months, revenue_total, payment_frequency
        )
        VALUES ($1, $2, $3::date, $4, $5, $6)
        RETURNING id
    `,
			clientID,
			salesID,
			*req.ContractStartDate,
			*req.ContractDurationMonths,
			*req.UpsellRevenue,
			*req.ContractFrequency,
		).Scan(&newContractID)

		if err != nil {
			http.Error(w, "failed to create contract: "+err.Error(), 500)
			return
		}

		// first payment date / spacing is handled inside the cashflow helper

		// insert the full schedule for the new contract (atomic within tx)
		sd, err := time.Parse("2006-01-02", *req.ContractStartDate)
		if err != nil {
			http.Error(w, "invalid contract_start_date", 400)
			return
		}
		endDate := sd.AddDate(0, *req.ContractDurationMonths, 0)

		if err := insertCashflowEntriesTx(
			tx,
			*newContractID,
			sd,
			endDate,
			*req.UpsellRevenue,
			*req.ContractFrequency,
		); err != nil {
			http.Error(w, "failed to create cashflow entries: "+err.Error(), 500)
			return
		}

	}

	// -----------------------
	// Insert or update upsell
	// -----------------------

	var upsellID int

	if existingUpsellID == nil {
		// CREATE
		err = tx.QueryRow(`
            INSERT INTO contract_upsells
                (sales_process_id, client_id, upsell_date, upsell_result,
                 upsell_revenue, previous_contract_id, new_contract_id)
            VALUES ($1,$2,$3,$4,$5,$6,$7)
            RETURNING id
        `,
			salesID,
			clientID,
			req.UpsellDate,    // may be NULL
			req.UpsellResult,  // may be NULL
			req.UpsellRevenue, // may be NULL
			prevContractID,
			newContractID,
		).Scan(&upsellID)
	} else {
		// UPDATE
		err = tx.QueryRow(`
            UPDATE contract_upsells
            SET
                upsell_date   = COALESCE($2, upsell_date),
                upsell_result = COALESCE($3, upsell_result),
                upsell_revenue = COALESCE($4, upsell_revenue),
                new_contract_id = COALESCE($5, new_contract_id)
            WHERE id = $1
            RETURNING id
        `,
			*existingUpsellID,
			req.UpsellDate,
			req.UpsellResult,
			req.UpsellRevenue,
			newContractID,
		).Scan(&upsellID)
	}

	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, "commit: "+err.Error(), 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"upsell_id":       upsellID,
		"updated":         existingUpsellID != nil,
		"new_contract_id": newContractID,
	})
}

func (h *Handler) GetUpsellAnalytics(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var where []string
	var args []any
	idx := 1

	if v := q.Get("start_date"); v != "" {
		start, err := time.Parse("2006-01-02", v)
		if err != nil {
			http.Error(w, "invalid start_date (expected YYYY-MM-DD)", http.StatusBadRequest)
			return
		}
		where = append(where, "cu.upsell_date >= $"+strconv.Itoa(idx))
		args = append(args, start)
		idx++
	}
	if v := q.Get("end_date"); v != "" {
		end, err := time.Parse("2006-01-02", v)
		if err != nil {
			http.Error(w, "invalid end_date (expected YYYY-MM-DD)", http.StatusBadRequest)
			return
		}
		where = append(where, "cu.upsell_date <= $"+strconv.Itoa(idx))
		args = append(args, end)
		idx++
	}

	whereSQL := ""
	if len(where) > 0 {
		whereSQL = "WHERE " + strings.Join(where, " AND ")
	}

	var stats struct {
		VerlaengerungCount      int      `json:"verlangerung_count"`
		KeineVerlaengerungCount int      `json:"keine_verlangerung_count"`
		ScheduledCount          int      `json:"scheduled_count"`
		Verlaengerungsquote     *float64 `json:"verlangerungsquote"`
		UmsatzSum               float64  `json:"umsatz_sum"`
	}

	query := `
        SELECT
			COUNT(*) FILTER (WHERE upsell_result = 'verlaengerung')         AS verlangerung_count,
			COUNT(*) FILTER (WHERE upsell_result = 'keine_verlaengerung')  AS keine_verlangerung_count,
			COUNT(*) FILTER (WHERE upsell_result IS NULL)                  AS scheduled_count,

			ROUND(
				100.0 * COUNT(*) FILTER (WHERE upsell_result = 'verlaengerung')
				/ NULLIF(
					COUNT(*) FILTER (WHERE upsell_result IN ('verlaengerung','keine_verlaengerung')),
					0
				),
				1
			) AS verlangerungsquote,

			COALESCE(SUM(upsell_revenue), 0) AS umsatz_sum
		FROM contract_upsells cu
	` + whereSQL + `;
    `

	err := h.DB.QueryRow(query, args...).Scan(
		&stats.VerlaengerungCount,
		&stats.KeineVerlaengerungCount,
		&stats.ScheduledCount,
		&stats.Verlaengerungsquote,
		&stats.UmsatzSum,
	)

	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}
