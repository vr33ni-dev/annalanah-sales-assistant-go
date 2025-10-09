// api/cashflow.go
package api

import (
	"encoding/json"
	"net/http"
	"time"
)

type CashflowRow struct {
	Month     string  `json:"month"`     // YYYY-MM
	Confirmed float64 `json:"confirmed"` // invoiced or scheduled from contracts
	Potential float64 `json:"potential"` // open deals
}

func (h *Handler) CashflowForecast(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 6, 0) // exclusive

	// 🔧 read tunables from app_settings (defaults if not present)
	potentialMonths := h.getNumericSetting("potential_months", 6)
	potentialFlatEUR := h.getNumericSetting("potential_flat_eur", 900)

	rows, err := h.DB.Query(`
WITH months AS (
  SELECT to_char(d::date, 'YYYY-MM') AS ym,
         date_trunc('month', d)::date                        AS month_start,
         (date_trunc('month', d) + interval '1 month')::date AS month_end
  FROM generate_series($1::date, $2::date - interval '1 month', interval '1 month') AS d
),

-- A) Explicit cashflow entries inside window
entries AS (
  SELECT
    cf.contract_id,
    cf.due_date::date AS due_date,
    cf.amount::numeric AS amount
  FROM cashflow_entries cf
  WHERE cf.amount > 0
    AND cf.due_date >= $1::date
    AND cf.due_date <  $2::date
),

-- B) Full schedule derived from contracts (one due per payment period)
schedule AS (
  SELECT
    c.id            AS contract_id,
    gs::date        AS due_date,
    (
      (c.revenue_total / c.duration_months) *
      CASE c.payment_frequency
        WHEN 'monthly'    THEN 1
        WHEN 'bi-monthly' THEN 2
        WHEN 'quarterly'  THEN 3
        ELSE 1
      END
    )::numeric AS amount
  FROM contracts c
  JOIN LATERAL generate_series(
         date_trunc('month', c.start_date),
         date_trunc('month', c.start_date) + (c.duration_months - 1) * interval '1 month',
         CASE c.payment_frequency
           WHEN 'monthly'    THEN interval '1 month'
           WHEN 'bi-monthly' THEN interval '2 months'
           WHEN 'quarterly'  THEN interval '3 months'
           ELSE interval '1 month'
         END
       ) gs ON TRUE
),

-- C) Only those scheduled dues that do NOT already have an explicit entry
schedule_no_entry AS (
  SELECT s.contract_id, s.due_date, s.amount
  FROM schedule s
  LEFT JOIN cashflow_entries cfe
    ON cfe.contract_id = s.contract_id
   AND cfe.due_date::date = s.due_date
  WHERE s.due_date >= $1::date
    AND s.due_date <  $2::date
    AND cfe.id IS NULL
),

-- D) Confirmed = entries + (schedule without entries), aggregated by month
confirmed AS (
  SELECT m.ym, COALESCE(SUM(e.amount), 0)::numeric AS amt
  FROM months m
  LEFT JOIN entries e
    ON e.due_date >= m.month_start
   AND e.due_date <  m.month_end
  GROUP BY m.ym

  UNION ALL

  SELECT m.ym, COALESCE(SUM(s.amount), 0)::numeric AS amt
  FROM months m
  LEFT JOIN schedule_no_entry s
    ON s.due_date >= m.month_start
   AND s.due_date <  m.month_end
  GROUP BY m.ym
),

-- E) Potential from sales_process (unchanged logic, just parameterized)
potential AS (
  SELECT to_char(sp.zweitgespraech_date, 'YYYY-MM') AS ym,
         SUM(
           CASE
             WHEN c.id IS NOT NULL AND c.duration_months > 0
               THEN (c.revenue_total / c.duration_months)::numeric
             WHEN sp.revenue IS NOT NULL AND sp.revenue > 0
               THEN (sp.revenue / $3)::numeric        -- $3 = potential_months
             ELSE $4::numeric                          -- $4 = potential_flat_eur
           END
         ) AS amt
  FROM sales_process sp
  LEFT JOIN contracts c ON c.sales_process_id = sp.id
  WHERE sp.stage = 'zweitgespraech'
    AND COALESCE(sp.abschluss, false) = false
    AND sp.zweitgespraech_result = true
    AND sp.zweitgespraech_date IS NOT NULL
    AND sp.zweitgespraech_date >= $1::date AND sp.zweitgespraech_date < $2::date
  GROUP BY 1
),

-- F) Collapse the two confirmed parts into one number per month
confirmed_collapsed AS (
  SELECT ym, SUM(amt)::numeric AS amt
  FROM confirmed
  GROUP BY ym
),

joined AS (
  SELECT
    m.ym,
    COALESCE(cc.amt, 0) AS confirmed,
    COALESCE(pt.amt, 0) AS potential
  FROM months m
  LEFT JOIN confirmed_collapsed cc ON cc.ym = m.ym
  LEFT JOIN potential pt          ON pt.ym = m.ym
)
SELECT ym AS month, confirmed, potential
FROM joined
ORDER BY month;
`, start, end, potentialMonths, potentialFlatEUR)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	var out []CashflowRow
	for rows.Next() {
		var row CashflowRow
		if err := rows.Scan(&row.Month, &row.Confirmed, &row.Potential); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		out = append(out, row)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
