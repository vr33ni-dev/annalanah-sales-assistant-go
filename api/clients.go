package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"
)

type Client struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	Email         string `json:"email"`
	Phone         string `json:"phone"`
	Source        string `json:"source"`
	SourceStageID *int   `json:"source_stage_id,omitempty"`
	Status        string `json:"status"` //  "active", "lost", "follow_up_scheduled", "awaiting_response", "inactive" etc.
}

type Handler struct {
	DB *sql.DB
}

// GET /api/clients
func (h *Handler) ListClients(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	type ClientResponse struct {
		ID              int64  `json:"id"`
		Name            string `json:"name"`
		Email           string `json:"email"`
		Phone           string `json:"phone"`
		Source          string `json:"source"`
		SourceStageName string `json:"source_stage_name"`
		Status          string `json:"status"`
	}
	rows, err := h.DB.QueryContext(ctx, `
        SELECT 
            c.id,
            c.name,
            c.email,
            c.phone,
						c.source,
            COALESCE(s.name, '') AS source_stage_name,
						c.status
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
	for rows.Next() {
		var c ClientResponse
		if err := rows.Scan(&c.ID, &c.Name, &c.Email, &c.Phone, &c.Source, &c.SourceStageName, &c.Status); err != nil {
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
		"INSERT INTO clients (name, email, phone, source, source_stage_id, status) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id",
		c.Name, c.Email, c.Phone, c.Source, c.SourceStageID, c.Status).Scan(&c.ID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(c)
}
