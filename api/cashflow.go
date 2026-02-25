// api/cashflow.go
package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type CashflowRow struct {
	Month     string  `json:"month"`     // YYYY-MM
	Confirmed float64 `json:"confirmed"` // invoiced or scheduled from contracts
	Potential float64 `json:"potential"` // open deals
}

// GET /api/cashflow/entries
// Supports filters: contract_id, client_id, status, start_date, end_date
// Pagination: page (default 1), per_page (default 50, max 500)
func (h *Handler) ListCashflowEntries(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var where []string
	var args []interface{}
	idx := 1

	// filters
	if v := q.Get("contract_id"); v != "" {
		where = append(where, "ce.contract_id = $"+strconv.Itoa(idx))
		args = append(args, v)
		idx++
	}
	if v := q.Get("client_id"); v != "" {
		where = append(where, "c.client_id = $"+strconv.Itoa(idx))
		args = append(args, v)
		idx++
	}
	if v := q.Get("status"); v != "" {
		where = append(where, "ce.status = $"+strconv.Itoa(idx))
		args = append(args, v)
		idx++
	}
	if v := q.Get("start_date"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			where = append(where, "ce.due_date >= $"+strconv.Itoa(idx))
			args = append(args, t)
			idx++
		}
	}
	if v := q.Get("end_date"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			where = append(where, "ce.due_date <= $"+strconv.Itoa(idx))
			args = append(args, t)
			idx++
		}
	}

	// pagination
	page := 1
	perPage := 50
	if v := q.Get("page"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 {
			page = p
		}
	}
	if v := q.Get("per_page"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 {
			if p > 500 {
				perPage = 500
			} else {
				perPage = p
			}
		}
	}

	whereSQL := ""
	if len(where) > 0 {
		whereSQL = "WHERE " + strings.Join(where, " AND ")
	}

	// count total
	countQuery := "SELECT COUNT(*) FROM cashflow_entries ce LEFT JOIN contracts c ON c.id = ce.contract_id " + whereSQL
	var total int
	if err := h.DB.QueryRow(countQuery, args...).Scan(&total); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	offset := (page - 1) * perPage

	dataQuery := `SELECT ce.id, ce.contract_id, ce.due_date, ce.amount, ce.status, ce.updated_at
        FROM cashflow_entries ce
        LEFT JOIN contracts c ON c.id = ce.contract_id
        ` + whereSQL + `
        ORDER BY ce.due_date ASC, ce.id ASC
        LIMIT $` + strconv.Itoa(idx) + ` OFFSET $` + strconv.Itoa(idx+1)

	args = append(args, perPage, offset)

	rows, err := h.DB.Query(dataQuery, args...)
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

	resp := map[string]interface{}{
		"data": out,
		"meta": map[string]interface{}{
			"total":    total,
			"page":     page,
			"per_page": perPage,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// GET /api/cashflow/forecast
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
	// it only controls which months are returned (via ?months=). Here we read
	// the average revenue per contract setting used as the potential fallback.
	avgRevenuePerContract := h.getNumericSetting("avg_revenue_per_contract", 0)

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
		rows, err = h.DB.Query(query, start, end, avgRevenuePerContract, *contractID)
	} else {
		rows, err = h.DB.Query(query, start, end, avgRevenuePerContract, nil)
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

// GET /api/cashflow/dashboard
// Returns dashboard metrics used by the frontend:
// - avg_monthly_ytd: average realized (paid) cash-in per month YTD
// - months_elapsed_ytd: number of months elapsed in YTD (Jan..now, inclusive)
// - ytd_paid_amount: total paid amount YTD
// - confirmed_next3: array of { month: YYYY-MM, confirmed: amount } for next 3 months
// - avg_confirmed_next3: average confirmed amount per month over next 3 months
func (h *Handler) CashflowMetrics(w http.ResponseWriter, r *http.Request) {
	now := time.Now()

	// YTD window
	ytdStart := time.Date(now.Year(), time.January, 1, 0, 0, 0, 0, time.UTC)
	ytdEnd := now

	var ytdPaid float64
	// Count all cashflow_entries in the YTD window except those explicitly marked 'not paid'.
	// This treats statuses like 'paid', 'pending', or NULL as contributing to YTD sums.
	if err := h.DB.QueryRow(`SELECT COALESCE(SUM(amount) FILTER (WHERE COALESCE(status,'') <> 'not paid' AND due_date::date >= $1 AND due_date::date <= $2), 0) FROM cashflow_entries`, ytdStart, ytdEnd).Scan(&ytdPaid); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	monthsElapsed := int(now.Month()) // Jan=1 => months elapsed in year
	var avgMonthlyYtd float64
	if monthsElapsed > 0 {
		avgMonthlyYtd = ytdPaid / float64(monthsElapsed)
	}

	// Next 3 months confirmed (reuse forecast approach for confirmed amounts)
	startMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	endMonth := startMonth.AddDate(0, 3, 0) // exclusive

	query := `WITH months AS (
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
)
SELECT ym AS month, SUM(amt)::numeric AS confirmed
FROM confirmed
GROUP BY ym
ORDER BY month;`

	rows, err := h.DB.Query(query, startMonth, endMonth)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type MonthConfirmed struct {
		Month     string  `json:"month"`
		Confirmed float64 `json:"confirmed"`
	}

	var list []MonthConfirmed
	var sumNext3 float64
	for rows.Next() {
		var m MonthConfirmed
		if err := rows.Scan(&m.Month, &m.Confirmed); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		list = append(list, m)
		sumNext3 += m.Confirmed
	}

	var avgConfirmedNext3 float64
	if len(list) > 0 {
		avgConfirmedNext3 = sumNext3 / float64(len(list))
	}

	resp := map[string]interface{}{
		"avg_monthly_ytd":     avgMonthlyYtd,
		"months_elapsed_ytd":  monthsElapsed,
		"ytd_paid_amount":     ytdPaid,
		"confirmed_next3":     list,
		"avg_confirmed_next3": avgConfirmedNext3,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
