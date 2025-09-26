package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

type Contract struct {
	ID             int     `json:"id"`
	ClientID       int     `json:"client_id"`
	SalesProcessID int     `json:"sales_process_id"`
	StartDate      string  `json:"start_date"`
	EndDate        *string `json:"end_date,omitempty"`
	DurationMonths int     `json:"duration_months"`
	RevenueTotal   float64 `json:"revenue_total"`
	PaymentFreq    string  `json:"payment_frequency"`
}

// GET /api/contracts
func (h *Handler) ListContracts(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.Query(`
		SELECT id, client_id, sales_process_id, start_date, end_date, duration_months, revenue_total, payment_frequency
		FROM contracts`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var contracts []Contract
	for rows.Next() {
		var c Contract
		if err := rows.Scan(
			&c.ID,
			&c.ClientID,
			&c.SalesProcessID,
			&c.StartDate,
			&c.EndDate,
			&c.DurationMonths,
			&c.RevenueTotal,
			&c.PaymentFreq,
		); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		contracts = append(contracts, c)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(contracts)
}

// POST /api/contracts
func (h *Handler) CreateContract(w http.ResponseWriter, r *http.Request) {
	var c Contract
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	err := h.DB.QueryRow(
		`INSERT INTO contracts (client_id, sales_process_id, start_date, end_date, duration_months, revenue_total, payment_frequency)
		 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		c.ClientID,
		c.SalesProcessID,
		c.StartDate,
		c.EndDate,
		c.DurationMonths,
		c.RevenueTotal,
		c.PaymentFreq,
	).Scan(&c.ID)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(c)
}

// PATCH /api/contracts/{id}
func (h *Handler) UpdateContract(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid contract id", http.StatusBadRequest)
		return
	}

	var c Contract
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	_, err = h.DB.Exec(`
		UPDATE contracts
		SET end_date = $1, revenue_total = $2
		WHERE id = $3`,
		c.EndDate, c.RevenueTotal, id,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
