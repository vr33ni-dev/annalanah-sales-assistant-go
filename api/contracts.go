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

type ContractResponse struct {
	ID              int     `json:"id"`
	ClientID        int     `json:"client_id"`
	ClientName      string  `json:"client_name"`
	SalesProcessID  int     `json:"sales_process_id"`
	StartDate       string  `json:"start_date"`
	EndDate         *string `json:"end_date,omitempty"`
	DurationMonths  int     `json:"duration_months"`
	RevenueTotal    float64 `json:"revenue_total"`
	PaymentFreq     string  `json:"payment_frequency"`
	MonthlyAmount   float64 `json:"monthly_amount"`
	PaidMonths      int     `json:"paid_months"`
	PaidAmountTotal float64 `json:"paid_amount_total"`
	NextDueDate     *string `json:"next_due_date,omitempty"`
}

// GET /api/contracts
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
  c.end_date,
  c.duration_months,
  c.revenue_total,
  c.payment_frequency,

  -- monthly_amount
  CASE WHEN c.duration_months > 0
       THEN (c.revenue_total / c.duration_months)
       ELSE 0
  END AS monthly_amount,

  -- paid_months = periods_paid * period length (1/2/3 months)
  (
    COALESCE(p.periods_paid, 0) *
    CASE c.payment_frequency
      WHEN 'monthly'    THEN 1
      WHEN 'bi-monthly' THEN 2
      WHEN 'quarterly'  THEN 3
    END
  ) AS paid_months,

  COALESCE(p.paid_amount_total, 0)::numeric AS paid_amount_total,

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
			&x.StartDate, &x.EndDate, &x.DurationMonths, &x.RevenueTotal, &x.PaymentFreq,
			&x.MonthlyAmount, &x.PaidMonths, &x.PaidAmountTotal, &x.NextDueDate,
		); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		out = append(out, x)
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
