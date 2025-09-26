package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

type SalesProcess struct {
	ID                   int      `json:"id"`
	ClientID             int      `json:"client_id"`
	Stage                string   `json:"stage"`
	ZweitgespraechDate   *string  `json:"zweitgespraech_date"`
	ZweitgespraechResult *bool    `json:"zweitgespraech_result"`
	Abschluss            *bool    `json:"abschluss"`
	Revenue              *float64 `json:"revenue"`
	StageID              *int     `json:"stage_id"`
}

// GET /api/sales
func (h *Handler) ListSalesProcesses(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.Query(`
		SELECT id, client_id, stage, zweitgespraech_date, zweitgespraech_result, abschluss, revenue, stage_id
		FROM sales_process`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var processes []SalesProcess
	for rows.Next() {
		var sp SalesProcess
		if err := rows.Scan(
			&sp.ID,
			&sp.ClientID,
			&sp.Stage,
			&sp.ZweitgespraechDate,
			&sp.ZweitgespraechResult,
			&sp.Abschluss,
			&sp.Revenue,
			&sp.StageID,
		); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		processes = append(processes, sp)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(processes)
}

// POST /api/sales
func (h *Handler) CreateSalesProcess(w http.ResponseWriter, r *http.Request) {
	var sp SalesProcess
	if err := json.NewDecoder(r.Body).Decode(&sp); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Before inserting, check if this client already has a sales process
	var exists bool
	err := h.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM sales_process WHERE client_id = $1)", sp.ClientID).Scan(&exists)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if exists {
		http.Error(w, "this client already has a sales process (only one allowed)", http.StatusBadRequest)
		return
	}

	err = h.DB.QueryRow(
		`INSERT INTO sales_process (client_id, stage, zweitgespraech_date, zweitgespraech_result, abschluss, revenue, stage_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		sp.ClientID,
		sp.Stage,
		sp.ZweitgespraechDate,
		sp.ZweitgespraechResult,
		sp.Abschluss,
		sp.Revenue,
		sp.StageID,
	).Scan(&sp.ID)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sp)
}

// PATCH /api/sales/{id}
func (h *Handler) UpdateSalesProcess(w http.ResponseWriter, r *http.Request) {
	// Get id from URL and convert to int
	idStr := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid sales process id", http.StatusBadRequest)
		return
	}

	var sp SalesProcess
	if err := json.NewDecoder(r.Body).Decode(&sp); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Update DB
	_, err = h.DB.Exec(`
		UPDATE sales_process
		SET zweitgespraech_result = $1,
		    abschluss = $2,
		    revenue = $3
		WHERE id = $4`,
		sp.ZweitgespraechResult,
		sp.Abschluss,
		sp.Revenue,
		id,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Return updated record
	row := h.DB.QueryRow(`
		SELECT id, client_id, stage, zweitgespraech_date, zweitgespraech_result, abschluss, revenue, stage_id
		FROM sales_process
		WHERE id = $1`, id)

	var updated SalesProcess
	if err := row.Scan(
		&updated.ID,
		&updated.ClientID,
		&updated.Stage,
		&updated.ZweitgespraechDate,
		&updated.ZweitgespraechResult,
		&updated.Abschluss,
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updated)
}

// POST /api/sales/start
func (h *Handler) StartSalesProcess(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name               string  `json:"name"`
		Email              string  `json:"email"`
		Phone              string  `json:"phone"`
		Source             string  `json:"source"`
		SourceStageID      *int    `json:"source_stage_id,omitempty"`
		ZweitgespraechDate *string `json:"zweitgespraech_date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	tx, err := h.DB.Begin()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	// 1. Insert client
	var clientID int
	err = tx.QueryRow(
		`INSERT INTO clients (name, email, phone, source, source_stage_id)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		req.Name, req.Email, req.Phone, req.Source, req.SourceStageID,
	).Scan(&clientID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 2. Insert sales process
	var salesProcessID int
	err = tx.QueryRow(
		`INSERT INTO sales_process (client_id, stage, zweitgespraech_date)
		 VALUES ($1, 'zweitgespraech', $2) RETURNING id`,
		clientID, req.ZweitgespraechDate,
	).Scan(&salesProcessID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 3. Response
	resp := map[string]interface{}{
		"sales_process_id": salesProcessID,
		"client": map[string]interface{}{
			"id":              clientID,
			"name":            req.Name,
			"email":           req.Email,
			"phone":           req.Phone,
			"source":          req.Source,
			"source_stage_id": req.SourceStageID,
		},
		"sales_process": map[string]interface{}{
			"id":                    salesProcessID,
			"client_id":             clientID,
			"stage":                 "zweitgespraech",
			"zweitgespraech_date":   req.ZweitgespraechDate,
			"zweitgespraech_result": nil,
			"abschluss":             nil,
			"revenue":               nil,
			"stage_id":              nil,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
