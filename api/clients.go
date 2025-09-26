package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
)

type Client struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	Email         string `json:"email"`
	Source        string `json:"source"`
	SourceStageID *int   `json:"source_stage_id,omitempty"`
}

type Handler struct {
	DB *sql.DB
}

// GET /api/clients
func (h *Handler) ListClients(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.Query("SELECT id, name, email, source, source_stage_id FROM clients")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	var clients []Client
	for rows.Next() {
		var c Client
		if err := rows.Scan(&c.ID, &c.Name, &c.Email, &c.Source, &c.SourceStageID); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		clients = append(clients, c)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(clients)
}

// POST /api/clients
func (h *Handler) CreateClient(w http.ResponseWriter, r *http.Request) {
	var c Client
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	err := h.DB.QueryRow(
		"INSERT INTO clients (name, email, source, source_stage_id) VALUES ($1, $2, $3, $4) RETURNING id",
		c.Name, c.Email, c.Source, c.SourceStageID).Scan(&c.ID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(c)
}
