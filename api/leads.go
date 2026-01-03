package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/lib/pq"
)

type LeadResponse struct {
	ID              int     `json:"id"`
	Name            string  `json:"name"`
	Email           string  `json:"email"`
	Phone           string  `json:"phone"`
	Source          string  `json:"source"`
	SourceStageID   *int    `json:"source_stage_id,omitempty"`
	SourceStageName *string `json:"source_stage_name,omitempty"`
	Converted       bool    `json:"converted"`
	CreatedAt       *string `json:"created_at,omitempty"`
}

// GET /api/leads
func (h *Handler) ListLeads(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	rows, err := h.DB.QueryContext(ctx, `
		SELECT
			l.id,
			l.name,
			l.email,
			l.phone,
			l.source,
			l.source_stage_id,
			s.name AS source_stage_name,
			l.converted,
			l.created_at
		FROM leads l
		LEFT JOIN stages s ON s.id = l.source_stage_id
		ORDER BY l.id;
`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	leads := make([]LeadResponse, 0, 64)
	for rows.Next() {
		var lr LeadResponse
		var createdAt sql.NullTime
		var emailNS, phoneNS sql.NullString
		var sourceStageID sql.NullInt64
		var sourceStageName sql.NullString

		if err := rows.Scan(
			&lr.ID,
			&lr.Name,
			&emailNS,
			&phoneNS,
			&lr.Source,
			&sourceStageID,
			&sourceStageName,
			&lr.Converted,
			&createdAt,
		); err != nil {
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}

		if emailNS.Valid {
			lr.Email = emailNS.String
		} else {
			lr.Email = ""
		}

		if phoneNS.Valid {
			lr.Phone = phoneNS.String
		} else {
			lr.Phone = ""
		}

		if sourceStageID.Valid {
			v := int(sourceStageID.Int64)
			lr.SourceStageID = &v
		}

		if sourceStageName.Valid {
			lr.SourceStageName = &sourceStageName.String
		}

		if createdAt.Valid {
			s := createdAt.Time.Format(time.RFC3339)
			lr.CreatedAt = &s
		}

		leads = append(leads, lr)
	}

	if leads == nil {
		leads = []LeadResponse{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(leads)
}

// POST /api/leads
func (h *Handler) CreateLead(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var payload struct {
		Name            string  `json:"name"`
		Email           *string `json:"email"`
		Phone           *string `json:"phone"`
		Source          string  `json:"source"`
		SourceStageID   *int    `json:"source_stage_id,omitempty"`
		SourceStageName *string `json:"source_stage_name,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	// Resolve stage id
	var stageID sql.NullInt64
	if payload.SourceStageID != nil {
		stageID = sql.NullInt64{Int64: int64(*payload.SourceStageID), Valid: true}
	} else if payload.SourceStageName != nil {
		var sid int64
		if err := h.DB.QueryRowContext(
			ctx,
			`SELECT id FROM stages WHERE name = $1 LIMIT 1`,
			*payload.SourceStageName,
		).Scan(&sid); err == nil {
			stageID = sql.NullInt64{Int64: sid, Valid: true}
		}
	}

	var lr LeadResponse
	var createdAt sql.NullTime

	// ----------------------------------------------------
	// Attempt insert
	// ----------------------------------------------------
	err := h.DB.QueryRowContext(ctx,
		`INSERT INTO leads (name, email, phone, source, source_stage_id)
         VALUES ($1, $2, $3, $4, $5)
         RETURNING id, created_at`,
		payload.Name,
		payload.Email,
		payload.Phone,
		payload.Source,
		stageID,
	).Scan(&lr.ID, &createdAt)

	// ----------------------------------------------------
	// Handle duplicate email → return existing lead
	// ----------------------------------------------------
	if err != nil {
		if pgErr, ok := err.(*pq.Error); ok && pgErr.Code == "23505" {
			// unique_lead_email violated → fetch existing lead
			if payload.Email == nil || *payload.Email == "" {
				http.Error(w, "duplicate lead", http.StatusConflict)
				return
			}

			row := h.DB.QueryRowContext(ctx, `
				SELECT
					l.id,
					l.name,
					l.email,
					l.phone,
					l.source,
					COALESCE(s.name, '') AS source_stage_name,
					l.converted,
					l.created_at
				FROM leads l
				LEFT JOIN stages s ON s.id = l.source_stage_id
				WHERE LOWER(l.email) = LOWER($1)
				LIMIT 1
			`, *payload.Email)

			var emailNS, phoneNS sql.NullString
			var createdAtNS sql.NullTime

			if err := row.Scan(
				&lr.ID,
				&lr.Name,
				&emailNS,
				&phoneNS,
				&lr.Source,
				&lr.SourceStageName,
				&lr.Converted,
				&createdAtNS,
			); err != nil {
				http.Error(w, "failed fetching existing lead", http.StatusInternalServerError)
				return
			}

			if emailNS.Valid {
				lr.Email = emailNS.String
			}
			if phoneNS.Valid {
				lr.Phone = phoneNS.String
			}
			if createdAtNS.Valid {
				s := createdAtNS.Time.Format(time.RFC3339)
				lr.CreatedAt = &s
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(lr)
			return
		}

		http.Error(w, "failed creating lead: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// ----------------------------------------------------
	// Successful insert → build response
	// ----------------------------------------------------
	lr.Name = payload.Name
	if payload.Email != nil {
		lr.Email = *payload.Email
	}
	if payload.Phone != nil {
		lr.Phone = *payload.Phone
	}
	lr.Source = payload.Source
	if createdAt.Valid {
		s := createdAt.Time.Format(time.RFC3339)
		lr.CreatedAt = &s
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(lr)
}

// PATCH /api/leads/{id}
func (h *Handler) UpdateLead(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	idStr := chi.URLParam(r, "id")
	leadID, err := strconv.Atoi(idStr)
	if err != nil || leadID <= 0 {
		http.Error(w, "invalid lead id", http.StatusBadRequest)
		return
	}

	var payload struct {
		Name            *string `json:"name"`
		Email           *string `json:"email"`
		Phone           *string `json:"phone"`
		Source          *string `json:"source"`
		SourceStageID   *int    `json:"source_stage_id,omitempty"`
		SourceStageName *string `json:"source_stage_name,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	if payload.Name == nil && payload.Email == nil && payload.Phone == nil && payload.Source == nil && payload.SourceStageID == nil && payload.SourceStageName == nil {
		http.Error(w, "no fields to update", http.StatusBadRequest)
		return
	}

	// Resolve stage id if provided by name
	var newStage sql.NullInt64
	if payload.SourceStageID != nil {
		newStage = sql.NullInt64{Int64: int64(*payload.SourceStageID), Valid: true}
	} else if payload.SourceStageName != nil {
		var sid int64
		if err := h.DB.QueryRowContext(ctx, `SELECT id FROM stages WHERE name = $1 LIMIT 1`, *payload.SourceStageName).Scan(&sid); err == nil {
			newStage = sql.NullInt64{Int64: sid, Valid: true}
		}
	}

	// Build update using COALESCE for provided pointers (NULL means no change)
	row := h.DB.QueryRowContext(ctx, `
        UPDATE leads SET
            name = COALESCE($1, name),
            email = COALESCE($2, email),
            phone = COALESCE($3, phone),
            source = COALESCE($4, source),
            source_stage_id = COALESCE($5, source_stage_id)
        WHERE id = $6
        RETURNING id, name, email, phone, source,
                  COALESCE((SELECT name FROM stages WHERE id = source_stage_id), '') AS source_stage_name,
                  created_at
    `, payload.Name, payload.Email, payload.Phone, payload.Source, newStage, leadID)

	var lr LeadResponse
	var createdAt sql.NullTime
	var emailNS, phoneNS sql.NullString

	if err := row.Scan(&lr.ID, &lr.Name, &emailNS, &phoneNS, &lr.Source, &lr.SourceStageName, &createdAt); err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "lead not found", http.StatusNotFound)
			return
		}
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	if createdAt.Valid {
		s := createdAt.Time.Format(time.RFC3339)
		lr.CreatedAt = &s
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(lr)
}

// DELETE /api/leads/{id}
func (h *Handler) DeleteLead(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	idStr := chi.URLParam(r, "id")
	leadID, err := strconv.Atoi(idStr)
	if err != nil || leadID <= 0 {
		http.Error(w, "invalid lead id", http.StatusBadRequest)
		return
	}

	res, err := h.DB.ExecContext(ctx, `DELETE FROM leads WHERE id = $1`, leadID)
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		http.Error(w, "lead not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// POST /api/leads/{id}/convert
// Creates a client from the lead (SELECT ... FROM leads WHERE id = $1), creates a sales_process
// with stage = 'follow_up' and sets lead_id. Returns { "client_id": ..., "sales_process_id": ... }.
// Idempotent: if sales_process already exists for the client, returns the existing id.
func (h *Handler) ConvertLead(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	idStr := chi.URLParam(r, "id")
	leadID, err := strconv.Atoi(idStr)
	if err != nil || leadID <= 0 {
		http.Error(w, "invalid lead id", http.StatusBadRequest)
		return
	}

	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	// load lead data
	var name string
	var email, phone sql.NullString
	var source string
	var sourceStage sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT name, email, phone, source, source_stage_id FROM leads WHERE id = $1`, leadID).
		Scan(&name, &email, &phone, &source, &sourceStage); err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "lead not found", http.StatusNotFound)
			return
		}
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	var emailPtr, phonePtr *string
	if email.Valid {
		s := email.String
		emailPtr = &s
	}
	if phone.Valid {
		s := phone.String
		phonePtr = &s
	}
	var stagePtr *int
	if sourceStage.Valid {
		v := int(sourceStage.Int64)
		stagePtr = &v
	}

	clientID, salesID, err := h.createClientAndSalesProcessTx(ctx, tx, name, emailPtr, phonePtr, source, stagePtr, nil, &leadID)
	if err != nil {
		// Better handling: distinguish which unique constraint failed.
		if pgErr, ok := err.(*pq.Error); ok {
			// sales_process unique constraint -> return existing sales_process id
			if pgErr.Constraint == "unique_client_sales" || pgErr.Code == "23505" && pgErr.Constraint == "" {
				_ = tx.Rollback()
				if err := h.DB.QueryRowContext(ctx, `SELECT id FROM sales_process WHERE client_id = $1`, clientID).Scan(&salesID); err != nil {
					http.Error(w, "server error", http.StatusInternalServerError)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]int{"client_id": clientID, "sales_process_id": salesID})
				return
			}

			// client email unique constraint -> find existing client and create/return sales_process idempotently
			if pgErr.Constraint == "unique_client_email" {
				_ = tx.Rollback()
				// find existing client by the lead's email
				var existingClientID int
				if emailPtr == nil {
					// fallback: read email from lead
					var leadEmail sql.NullString
					if err := h.DB.QueryRowContext(ctx, `SELECT email FROM leads WHERE id = $1`, leadID).Scan(&leadEmail); err != nil || !leadEmail.Valid {
						http.Error(w, "server error", http.StatusInternalServerError)
						return
					}
					if err := h.DB.QueryRowContext(ctx, `SELECT id FROM clients WHERE email = $1`, leadEmail.String).Scan(&existingClientID); err != nil {
						http.Error(w, "server error", http.StatusInternalServerError)
						return
					}
				} else {
					if err := h.DB.QueryRowContext(ctx, `SELECT id FROM clients WHERE email = $1`, *emailPtr).Scan(&existingClientID); err != nil {
						http.Error(w, "server error", http.StatusInternalServerError)
						return
					}
				}

				// try to create a sales_process for the existing client (idempotent)
				var createdSalesID int
				if err := h.DB.QueryRowContext(ctx, `
                    INSERT INTO sales_process (client_id, stage, stage_id, lead_id)
                    VALUES ($1, 'follow_up', $2, $3)
                    RETURNING id
                `, existingClientID, stagePtr, leadID).Scan(&createdSalesID); err != nil {
					// if another process already created it, return the existing one
					if pgErr2, ok2 := err.(*pq.Error); ok2 && pgErr2.Code == "23505" {
						if err := h.DB.QueryRowContext(ctx, `SELECT id FROM sales_process WHERE client_id = $1`, existingClientID).Scan(&createdSalesID); err != nil {
							http.Error(w, "server error", http.StatusInternalServerError)
							return
						}
					} else {
						http.Error(w, "server error", http.StatusInternalServerError)
						return
					}
				}

				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]int{"client_id": existingClientID, "sales_process_id": createdSalesID})
				return
			}
		}

		http.Error(w, "conversion failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// mark lead as converted (audit fields) — keep inside same transaction
	if _, err := tx.ExecContext(ctx, `
        UPDATE leads
        SET converted = TRUE,
            converted_at = now(),
            converted_client_id = $1
        WHERE id = $2
    `, clientID, leadID); err != nil {
		http.Error(w, "failed updating lead conversion status", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]int{"client_id": clientID, "sales_process_id": salesID})
}
