// api/cashflow.go
package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
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
	// forecast window: default 6 months, but frontend may pass ?months=N to change
	displayMonths := 6
	if ms := r.URL.Query().Get("months"); ms != "" {
		if m, err := strconv.Atoi(ms); err == nil && m > 0 {
			displayMonths = m
		}
	}
	end := start.AddDate(0, displayMonths, 0) // exclusive

	// optionaler ?contract_id= Parameter
	var contractID *int
	if cidStr := r.URL.Query().Get("contract_id"); cidStr != "" {
		if cid, err := strconv.Atoi(cidStr); err == nil {
			contractID = &cid
		}
	}

	// Tunables aus app_settings (Standardwerte falls nicht vorhanden)
	// NOTE: `potential_months` is intentionally NOT used to divide revenue anymore;
	// it only controls which months are returned (via ?months=). Here we only
	// read the flat fallback amount.
	potentialFlatEUR := h.getNumericSetting("potential_flat_eur", 900)

	// 💡 Wichtig: NICHT "sql :=" als Variablenname benutzen → sonst Shadowing!
	query := `
WITH months AS (
  SELECT to_char(d::date, 'YYYY-MM') AS ym,
         date_trunc('month', d)::date                        AS month_start,
         (date_trunc('month', d) + interval '1 month')::date AS month_end
  FROM generate_series($1::date, $2::date - interval '1 month', interval '1 month') AS d
),
entries AS (
  SELECT cf.contract_id, cf.due_date::date AS due_date, cf.amount::numeric AS amount
  FROM cashflow_entries cf
  WHERE cf.amount > 0
    AND cf.due_date >= $1::date
    AND cf.due_date <  $2::date
    -- optionaler Filter
    AND ($4::int IS NULL OR cf.contract_id = $4)
),
schedule AS (
  SELECT c.id AS contract_id, gs::date AS due_date,
    ((c.revenue_total / c.duration_months) *
      CASE c.payment_frequency
        WHEN 'monthly' THEN 1
        WHEN 'bi-monthly' THEN 2
        WHEN 'quarterly' THEN 3
        WHEN 'bi-yearly' THEN 6
        WHEN 'one-time' THEN c.duration_months
        ELSE 1
      END
    )::numeric AS amount
  FROM contracts c
  JOIN LATERAL generate_series(
         date_trunc('month', c.start_date),
         date_trunc('month', c.start_date) + (c.duration_months - 1) * interval '1 month',
         CASE c.payment_frequency
           WHEN 'monthly' THEN interval '1 month'
           WHEN 'bi-monthly' THEN interval '2 months'
           WHEN 'quarterly' THEN interval '3 months'
           WHEN 'bi-yearly' THEN interval '6 months'
           WHEN 'one-time' THEN (c.duration_months * interval '1 month')
           ELSE interval '1 month'
         END
       ) gs ON TRUE
  WHERE ($4::int IS NULL OR c.id = $4)
),
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
potential AS (
  SELECT to_char(sp.follow_up_date, 'YYYY-MM') AS ym,
         SUM(
           CASE
             WHEN c.id IS NOT NULL AND c.duration_months > 0
               THEN (c.revenue_total / c.duration_months)::numeric
             WHEN sp.revenue IS NOT NULL AND sp.revenue > 0
               THEN sp.revenue::numeric
             ELSE $3::numeric
           END
         ) AS amt
  FROM sales_process sp
  LEFT JOIN contracts c ON c.sales_process_id = sp.id
  WHERE sp.stage = 'follow_up'
    AND COALESCE(sp.closed, false) = false
    AND (sp.follow_up_result IS NULL OR sp.follow_up_result = true)
    AND sp.follow_up_date IS NOT NULL
    AND sp.follow_up_date >= $1::date
    AND sp.follow_up_date <  $2::date
    -- Potenzialfilter nur, wenn kein contract_id explizit angegeben ist
    AND ($4::int IS NULL)
  GROUP BY 1
),
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
`

	var rows *sql.Rows
	var err error

	// Wenn contractID vorhanden → vierter Parameter wird gesetzt (see SQL param ordering)
	if contractID != nil {
		rows, err = h.DB.Query(query, start, end, potentialFlatEUR, *contractID)
	} else {
		rows, err = h.DB.Query(query, start, end, potentialFlatEUR, nil)
	}

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

func (h *Handler) GetNumericSettingForTest(key string, def float64) float64 {
	return h.getNumericSetting(key, def)
}
