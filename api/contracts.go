package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

type Contract struct {
	ID             int                    `json:"id"`
	ClientID       int                    `json:"client_id"`
	SalesProcessID int                    `json:"sales_process_id"`
	StartDate      string                 `json:"start_date"`
	CreatedAt      *string                `json:"created_at,omitempty"`
	EndDate        *string                `json:"end_date_computed,omitempty"`
	DurationMonths int                    `json:"duration_months"`
	RevenueTotal   float64                `json:"revenue_total"`
	PaymentFreq    string                 `json:"payment_frequency"`
	Comments       []CommentCreateRequest `json:"comments,omitempty"`
}

type UpdateContractRequest struct {
	StartDate      string                 `json:"start_date"`
	DurationMonths int                    `json:"duration_months"`
	RevenueTotal   float64                `json:"revenue_total"`
	PaymentFreq    string                 `json:"payment_frequency"`
	Comments       []CommentCreateRequest `json:"comments,omitempty"`
}

type ContractResponse struct {
	ID                int               `json:"id"`
	ClientID          int               `json:"client_id"`
	ClientName        string            `json:"client_name"`
	SalesProcessID    int               `json:"sales_process_id"`
	CreatedAt         *string           `json:"created_at,omitempty"`
	UpdatedAt         *string           `json:"updated_at,omitempty"`
	StartDate         string            `json:"start_date"`
	EndDate           *string           `json:"end_date_computed,omitempty"`
	DurationMonths    int               `json:"duration_months"`
	RevenueTotal      float64           `json:"revenue_total"`
	PaymentFreq       string            `json:"payment_frequency"`
	BaseMonthlyAmount float64           `json:"base_monthly_amount"`
	NextDueDate       *string           `json:"next_due_date,omitempty"`
	Comments          []CommentResponse `json:"comments,omitempty"`
}

// GET /api/contracts
func (h *Handler) ListContracts(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.Query(`
WITH paid AS (
  SELECT
    contract_id,
    COUNT(*)                           AS periods_paid,
    COALESCE(SUM(amount), 0)::numeric  AS paid_amount_total
  FROM cashflow_entries
  WHERE status = 'paid'
  GROUP BY contract_id
),
pending AS (
  SELECT
    contract_id,
    MIN(due_date)::date AS next_due_date_cf
  FROM cashflow_entries
  WHERE status IN ('pending','overdue')
  GROUP BY contract_id
)
SELECT
  c.id,
  c.client_id,
  cl.name AS client_name,
  c.sales_process_id,
  c.start_date,
	c.end_date_computed,
	c.created_at,
  c.duration_months,
  c.revenue_total,
  c.payment_frequency,

  -- base_monthly_amount (convenience value)
  CASE WHEN c.duration_months > 0
	  THEN (c.revenue_total / c.duration_months)
	  ELSE 0
  END AS base_monthly_amount,

  -- next_due_date: prefer pending/overdue; else derive the next slot if inside duration
  COALESCE(
    pn.next_due_date_cf,
    CASE
			WHEN (
				COALESCE(p.periods_paid, 0) *
				CASE c.payment_frequency
					WHEN 'monthly'    THEN 1
					WHEN 'bi-monthly' THEN 2
					WHEN 'quarterly'  THEN 3
					WHEN 'bi-yearly'  THEN 6
					WHEN 'one-time'   THEN c.duration_months
					ELSE 1
				END
			) >= c.duration_months
        THEN NULL
      ELSE
			(
				c.start_date
				+ make_interval(
						months =>
							(COALESCE(p.periods_paid, 0)::int *
							CASE c.payment_frequency
								WHEN 'monthly'    THEN 1
								WHEN 'bi-monthly' THEN 2
								WHEN 'quarterly'  THEN 3
								WHEN 'bi-yearly'  THEN 6
								WHEN 'one-time'   THEN c.duration_months
								ELSE 1
							END)
					)
			)::date

    END
  ) AS next_due_date
FROM contracts c
JOIN clients cl ON cl.id = c.client_id
LEFT JOIN paid    p  ON p.contract_id  = c.id
LEFT JOIN pending pn ON pn.contract_id = c.id
ORDER BY c.id;
`)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	var out []ContractResponse
	for rows.Next() {
		var x ContractResponse
		if err := rows.Scan(
			&x.ID, &x.ClientID, &x.ClientName, &x.SalesProcessID,
			&x.StartDate, &x.EndDate, &x.CreatedAt, &x.DurationMonths, &x.RevenueTotal, &x.PaymentFreq,
			&x.BaseMonthlyAmount, &x.NextDueDate,
		); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		// load comments for this contract
		commentRows, err := h.DB.Query(`
			SELECT id, author, body, metadata, created_at, updated_at
			FROM comments
			WHERE entity_type = 'contract' AND entity_id = $1
			ORDER BY created_at DESC
		`, x.ID)
		if err == nil {
			var comments []CommentResponse
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
					comments = append(comments, CommentResponse{
						ID: id, EntityType: "contract", EntityID: x.ID, Author: a, Body: body, Metadata: meta,
						CreatedAt: created.Format(time.RFC3339), UpdatedAt: updated.Format(time.RFC3339),
					})
				}
			}
			_ = commentRows.Close()
			if comments == nil {
				comments = []CommentResponse{}
			}
			x.Comments = comments
		}

		out = append(out, x)
	}

	if out == nil {
		out = []ContractResponse{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// GET /api/contracts/{id}/cashflow
func (h *Handler) ListContractCashflowEntries(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid contract id", http.StatusBadRequest)
		return
	}

	rows, err := h.DB.Query(`
		SELECT id, contract_id, due_date, amount, status, updated_at
		FROM cashflow_entries
		WHERE contract_id = $1
		ORDER BY due_date
	`, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type Entry struct {
		ID         int     `json:"id"`
		ContractID int     `json:"contract_id"`
		DueDate    *string `json:"due_date"`
		Amount     float64 `json:"amount"`
		Status     string  `json:"status"`
		UpdatedAt  *string `json:"updated_at,omitempty"`
	}

	var out []Entry
	for rows.Next() {
		var e Entry
		var due sql.NullTime
		var updated sql.NullTime
		if err := rows.Scan(&e.ID, &e.ContractID, &due, &e.Amount, &e.Status, &updated); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if due.Valid {
			s := due.Time.Format(time.RFC3339)
			e.DueDate = &s
		} else {
			e.DueDate = nil
		}
		if updated.Valid {
			tu := updated.Time.Format(time.RFC3339)
			e.UpdatedAt = &tu
		}
		out = append(out, e)
	}

	if out == nil {
		out = []Entry{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// POST /api/contracts
func (h *Handler) CreateContract(w http.ResponseWriter, r *http.Request) {
	var c Contract
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// normalize and validate payment frequency
	pf := strings.ToLower(strings.TrimSpace(c.PaymentFreq))
	if pf != "monthly" && pf != "bi-monthly" && pf != "quarterly" && pf != "one-time" && pf != "bi-yearly" {
		http.Error(w, "invalid payment_frequency (allowed: monthly, bi-monthly, quarterly, one-time, bi-yearly)", http.StatusBadRequest)
		return
	}
	if pf == "bi-yearly" && c.DurationMonths < 12 {
		http.Error(w, "bi-yearly payment frequency requires duration_months >= 12", http.StatusBadRequest)
		return
	}
	c.PaymentFreq = pf

	// create contract and its cashflow entries atomically
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var createdAt sql.NullTime
	err = tx.QueryRowContext(r.Context(), `
	INSERT INTO contracts
		(client_id, sales_process_id, start_date, duration_months, revenue_total, payment_frequency)
	VALUES ($1, $2, $3::date, $4, $5, $6)
	RETURNING id, created_at
`,
		c.ClientID,
		c.SalesProcessID,
		c.StartDate,
		c.DurationMonths,
		c.RevenueTotal,
		c.PaymentFreq,
	).Scan(&c.ID, &createdAt)

	if err != nil {
		tx.Rollback()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if createdAt.Valid {
		s := createdAt.Time.Format(time.RFC3339)
		c.CreatedAt = &s
	}

	// parse start date and insert scheduled cashflow entries
	sd, err := time.Parse("2006-01-02", c.StartDate)
	if err == nil && c.DurationMonths > 0 {
		if err := insertCashflowEntriesTx(tx, c.ID, sd, c.DurationMonths, c.RevenueTotal, c.PaymentFreq); err != nil {
			tx.Rollback()
			http.Error(w, "failed to insert cashflow entries: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// optionally insert comments submitted with the create request (non-fatal)
	if len(c.Comments) > 0 {
		_ = h.insertCommentsForEntity("contract", c.ID, c.Comments)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(c)
}

// PATCH /api/contracts/{id}
// PATCH /api/contracts/{id}
func (h *Handler) UpdateContract(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid contract id", http.StatusBadRequest)
		return
	}

	var req UpdateContractRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// normalize and validate payment frequency
	pf := strings.ToLower(strings.TrimSpace(req.PaymentFreq))
	if pf != "monthly" && pf != "bi-monthly" && pf != "quarterly" && pf != "one-time" && pf != "bi-yearly" {
		http.Error(w, "invalid payment_frequency (allowed: monthly, bi-monthly, quarterly, one-time, bi-yearly)", http.StatusBadRequest)
		return
	}
	if pf == "bi-yearly" && req.DurationMonths < 12 {
		http.Error(w, "bi-yearly payment frequency requires duration_months >= 12", http.StatusBadRequest)
		return
	}
	req.PaymentFreq = pf

	// Convert "YYYY-MM-DD" → time.Time
	t, err := time.Parse("2006-01-02", req.StartDate)
	if err != nil {
		http.Error(w, "invalid start_date (expected YYYY-MM-DD)", http.StatusBadRequest)
		return
	}

	_, err = h.DB.Exec(`
        UPDATE contracts
        SET 
            start_date = $1,
            duration_months = $2,
            revenue_total = $3,
            payment_frequency = $4
        WHERE id = $5
    `,
		t,
		req.DurationMonths,
		req.RevenueTotal,
		req.PaymentFreq,
		id,
	)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// optionally insert comments provided in the patch
	if req.Comments != nil && len(req.Comments) > 0 {
		if err := h.insertCommentsForEntity("contract", id, req.Comments); err != nil {
			// ignore failure
		}
	}

	w.WriteHeader(http.StatusNoContent)
}
