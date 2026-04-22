package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"
)

func (h *Handler) GetContractsInRange(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	startStr := q.Get("start_date")
	endStr := q.Get("end_date")
	typ := strings.ToLower(q.Get("type"))
	if typ != "neukunden" && typ != "verlaengerung" {
		writeJSONError(w, "type must be 'neukunden' or 'verlaengerung'", http.StatusBadRequest)
		return
	}

	var start, end sql.NullTime
	if startStr != "" {
		t, err := time.Parse("2006-01-02", startStr)
		if err != nil {
			writeJSONError(w, "invalid start_date (expected YYYY-MM-DD)", http.StatusBadRequest)
			return
		}
		start = sql.NullTime{Time: t, Valid: true}
	}
	if endStr != "" {
		t, err := time.Parse("2006-01-02", endStr)
		if err != nil {
			writeJSONError(w, "invalid end_date (expected YYYY-MM-DD)", http.StatusBadRequest)
			return
		}
		end = sql.NullTime{Time: t, Valid: true}
	}

	const mwst = 1.19

	type ContractRow struct {
		ContractID   int     `json:"contract_id"`
		ClientID     int     `json:"client_id"`
		ClientName   string  `json:"client_name"`
		StartDate    string  `json:"start_date"`
		EndDate      *string `json:"end_date,omitempty"`
		RevenueNetto float64 `json:"revenue_netto"`
	}
	var rows []ContractRow

	var sqlQuery string
	if typ == "neukunden" {
		// Neukunden: contracts that are NOT referenced as new_contract_id in contract_upsells with upsell_result='verlaengerung'
		sqlQuery = `
			SELECT c.id, c.client_id, cl.name, c.start_date, c.end_date, c.revenue_total
			FROM contracts c
			JOIN clients cl ON cl.id = c.client_id
			WHERE cl.status = 'active'
			  AND ($1::date IS NULL OR c.start_date >= $1::date)
			  AND ($2::date IS NULL OR c.start_date <= $2::date)
			  AND NOT EXISTS (
				SELECT 1 FROM contract_upsells cu
				WHERE cu.new_contract_id = c.id AND cu.upsell_result = 'verlaengerung'
			  )
			ORDER BY c.start_date DESC, c.id DESC
		`
	} else {
		// Verlaengerung: contracts that ARE referenced as new_contract_id in contract_upsells with upsell_result='verlaengerung'
		// Return upsell_revenue if present, else fallback to contract value
		sqlQuery = `
			SELECT c.id, c.client_id, cl.name, c.start_date, c.end_date,
				   COALESCE(cu.upsell_revenue, c.revenue_total) AS revenue
			FROM contracts c
			JOIN clients cl ON cl.id = c.client_id
			JOIN contract_upsells cu ON cu.new_contract_id = c.id AND cu.upsell_result = 'verlaengerung'
			WHERE cl.status = 'active'
			  AND ($1::date IS NULL OR c.start_date >= $1::date)
			  AND ($2::date IS NULL OR c.start_date <= $2::date)
			ORDER BY c.start_date DESC, c.id DESC
		`
	}

	var args []interface{}
	if start.Valid {
		args = append(args, start.Time)
	} else {
		args = append(args, nil)
	}
	if end.Valid {
		args = append(args, end.Time)
	} else {
		args = append(args, nil)
	}

	dbRows, err := h.DB.Query(sqlQuery, args...)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer dbRows.Close()

	for dbRows.Next() {
		var r ContractRow
		var endDate sql.NullString
		var revenueBrutto float64
		if err := dbRows.Scan(&r.ContractID, &r.ClientID, &r.ClientName, &r.StartDate, &endDate, &revenueBrutto); err != nil {
			writeJSONError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if endDate.Valid {
			r.EndDate = &endDate.String
		}
		r.RevenueNetto = revenueBrutto / mwst
		rows = append(rows, r)
	}
	if err := dbRows.Err(); err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rows)

	// GET /api/dashboard/kpis?start_date=YYYY-MM-DD&end_date=YYYY-MM-DD
	//
	// Returns all KPIs needed by the Dashboard page so the frontend does not have
}

// to cross-reference large lists (upsellsAll, allContracts, salesProcesses).
//
// Response fields:
//   - renewal_revenue           sum of upsell_revenue for verlaengerung in range
//   - new_customer_revenue      sum of revenue_total for first-period contracts started in range
//   - total_revenue             sum of revenue_total for all contracts (new + renewals) starting in the date range
//   - active_revenue            sum of revenue_total for contracts running today (start_date <= today <= end_date)
//   - clv_active_clients         sum of revenue_total across ALL periods of active clients (lifetime)
//   - clv_all_time               sum of revenue_total across ALL contracts, ALL clients, all time
//   - avg_vertragswert          active_revenue / active_contracts_count (nil if no active contracts)
//   - avg_clv_per_contract      clv_active_clients / active_contracts_count (nil if no active contracts)
//   - active_contracts_count    contracts running today: start_date <= today <= end_date
//   - won_new_count             new-customer sales processes won (by completed_at) in range
//   - decided_new_count         new-customer sales processes decided (won or lost) in range
//   - closing_rate_new          won_new_count / decided_new_count * 100 (nil if no decisions)
//   - verlaengerungsquote       renewal rate % (nil if no decided upsells)
//   - verlaengerung_count       count of successful renewals in range
//   - keine_verlaengerung_count count of non-renewals in range
func (h *Handler) GetDashboardKPIs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	var start, end sql.NullTime
	if v := q.Get("start_date"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			http.Error(w, "invalid start_date (expected YYYY-MM-DD)", http.StatusBadRequest)
			return
		}
		start = sql.NullTime{Time: t, Valid: true}
	}
	if v := q.Get("end_date"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			http.Error(w, "invalid end_date (expected YYYY-MM-DD)", http.StatusBadRequest)
			return
		}
		end = sql.NullTime{Time: t, Valid: true}
	}

	// ── 1. Upsell aggregates ────────────────────────────────────────────────
	var renewalRevenue float64
	var verlaengerungCount, keineVerlaengerungCount int
	var verlaengerungsquote sql.NullFloat64

	err := h.DB.QueryRow(`
		SELECT
			COALESCE(SUM(upsell_revenue) FILTER (WHERE upsell_result = 'verlaengerung'), 0),
			COUNT(*) FILTER (WHERE upsell_result = 'verlaengerung'),
			COUNT(*) FILTER (WHERE upsell_result = 'keine_verlaengerung'),
			ROUND(
				100.0 * COUNT(*) FILTER (WHERE upsell_result = 'verlaengerung')
				/ NULLIF(COUNT(*) FILTER (WHERE upsell_result IN ('verlaengerung','keine_verlaengerung')), 0),
				1
			)
		FROM contract_upsells cu
		WHERE ($1::date IS NULL OR cu.upsell_date >= $1::date)
		  AND ($2::date IS NULL OR cu.upsell_date <= $2::date)
	`, start, end).Scan(&renewalRevenue, &verlaengerungCount, &keineVerlaengerungCount, &verlaengerungsquote)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	// ── 2. Contract revenue ─────────────────────────────────────────────────
	// total_revenue = terminal contracts only (current active period per client).
	//   revenue_total / active_contracts_count = meaningful average Vertragswert.
	// clv_active_clients = all contract periods across active clients (historical + current).
	//   Shows lifetime relationship value.
	// new_customer_revenue = revenue of first-period contracts started in the date
	//   range (not a verlaengerung target).
	var totalRevenue, totalCLV, newCustomerRevenue float64

	// total_revenue = all contracts (new + renewals) whose start_date falls in the
	// selected date range. Matches what the dashboard "Umsatz im Zeitraum" chip shows.
	err = h.DB.QueryRow(`
		SELECT COALESCE(SUM(c.revenue_total), 0)
		FROM contracts c
		JOIN clients cl ON cl.id = c.client_id
		WHERE cl.status = 'active'
		  AND ($1::date IS NULL OR c.start_date >= $1::date)
		  AND ($2::date IS NULL OR c.start_date <= $2::date)
	`, start, end).Scan(&totalRevenue)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	err = h.DB.QueryRow(`
		SELECT COALESCE(SUM(c.revenue_total), 0)
		FROM contracts c
		JOIN clients cl ON cl.id = c.client_id
		WHERE cl.status = 'active'
	`).Scan(&totalCLV)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	err = h.DB.QueryRow(`
		SELECT COALESCE(SUM(c.revenue_total), 0)
		FROM contracts c
		JOIN clients cl ON cl.id = c.client_id
		WHERE cl.status = 'active'
		  AND ($1::date IS NULL OR c.start_date >= $1::date)
		  AND ($2::date IS NULL OR c.start_date <= $2::date)
		  AND NOT EXISTS (
			SELECT 1 FROM contract_upsells cu
			WHERE cu.new_contract_id = c.id
			  AND cu.upsell_result = 'verlaengerung'
		  )
	`, start, end).Scan(&newCustomerRevenue)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	// ── 2b. Gesamt CLV (all contracts, all clients, all time) ────────────────
	var gesamtCLV float64
	err = h.DB.QueryRow(`
		SELECT COALESCE(SUM(revenue_total), 0) FROM contracts
	`).Scan(&gesamtCLV)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	// ── 3. Active contracts — terminal period per chain, not yet ended ───────
	// Terminal = last in chain (not referenced as previous_contract_id in a
	// verlaengerung). This gives exactly one row per active client regardless
	// of whether their next planned period has started yet.
	var activeContractsCount int
	var activeRevenue float64

	err = h.DB.QueryRow(`
		SELECT COUNT(*),
		 COALESCE(SUM(c.revenue_total), 0)
		FROM contracts c
		JOIN clients cl ON cl.id = c.client_id
		WHERE (c.end_date IS NULL OR c.end_date >= CURRENT_DATE)
		  AND cl.status = 'active'
		  AND NOT EXISTS (
			SELECT 1 FROM contract_upsells
			WHERE previous_contract_id = c.id
			  AND upsell_result = 'verlaengerung'
		  )
	`).Scan(&activeContractsCount, &activeRevenue)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	// ── 4. Closing rate — new customers only ────────────────────────────────
	// A "renewal process" is one where the client already had an upsell
	// scheduled/completed before the follow-up call (matches frontend logic).
	// Won: closed=true AND completed_at in range.
	// Decided: closed=true OR (closed=false AND follow_up_result=true),
	//          bucketed by completed_at (won) or updated_at/follow_up_date (lost).
	var wonNewCount int

	err = h.DB.QueryRow(`
		SELECT COUNT(*)
		FROM sales_process sp
		JOIN clients cl ON cl.id = sp.client_id
		WHERE sp.closed = true
		  AND sp.follow_up_date IS NOT NULL
		  AND COALESCE(sp.is_imported_placeholder, false) = false
		  AND ($1::date IS NULL OR cl.completed_at::date >= $1::date)
		  AND ($2::date IS NULL OR cl.completed_at::date <= $2::date)
		  AND NOT EXISTS (
			SELECT 1 FROM contract_upsells cu
			WHERE cu.client_id = sp.client_id
			  AND cu.upsell_date IS NOT NULL
			  AND cu.upsell_date < sp.follow_up_date
		  )
	`, start, end).Scan(&wonNewCount)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	var decidedNewCount int

	err = h.DB.QueryRow(`
		SELECT COUNT(*)
		FROM sales_process sp
		JOIN clients cl ON cl.id = sp.client_id
		WHERE (sp.closed = true OR (sp.closed = false AND sp.follow_up_result = true))
		  AND sp.follow_up_date IS NOT NULL
		  AND COALESCE(sp.is_imported_placeholder, false) = false
		  AND ($1::date IS NULL OR (
			CASE WHEN sp.closed = true THEN cl.completed_at::date
			     ELSE COALESCE(sp.updated_at, sp.follow_up_date, sp.created_at)::date
			END
		  ) >= $1::date)
		  AND ($2::date IS NULL OR (
			CASE WHEN sp.closed = true THEN cl.completed_at::date
			     ELSE COALESCE(sp.updated_at, sp.follow_up_date, sp.created_at)::date
			END
		  ) <= $2::date)
		  AND NOT EXISTS (
			SELECT 1 FROM contract_upsells cu
			WHERE cu.client_id = sp.client_id
			  AND cu.upsell_date IS NOT NULL
			  AND cu.upsell_date < sp.follow_up_date
		  )
	`, start, end).Scan(&decidedNewCount)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	// Convert Brutto → Netto: stored prices include 19% MwSt (B2C)
	const mwst = 1.19
	renewalRevenue /= mwst
	newCustomerRevenue /= mwst
	totalRevenue /= mwst
	totalCLV /= mwst
	gesamtCLV /= mwst
	activeRevenue /= mwst

	var closingRateNew *float64
	if decidedNewCount > 0 {
		v := math.Round(float64(wonNewCount)/float64(decidedNewCount)*1000) / 10
		closingRateNew = &v
	}

	var vq *float64
	if verlaengerungsquote.Valid {
		vq = &verlaengerungsquote.Float64
	}

	var avgVertragswert, avgCLVPerContract *float64
	if activeContractsCount > 0 {
		v1 := activeRevenue / float64(activeContractsCount)
		v2 := totalCLV / float64(activeContractsCount)
		avgVertragswert = &v1
		avgCLVPerContract = &v2
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"renewal_revenue":           renewalRevenue,
		"new_customer_revenue":      newCustomerRevenue,
		"total_revenue":             totalRevenue,
		"active_revenue":            activeRevenue,
		"clv_active_clients":        totalCLV,
		"clv_all_time":              gesamtCLV,
		"avg_vertragswert":          avgVertragswert,
		"avg_clv_per_contract":      avgCLVPerContract,
		"active_contracts_count":    activeContractsCount,
		"won_new_count":             wonNewCount,
		"decided_new_count":         decidedNewCount,
		"closing_rate_new":          closingRateNew,
		"verlaengerungsquote":       vq,
		"verlaengerung_count":       verlaengerungCount,
		"keine_verlaengerung_count": keineVerlaengerungCount,
	})
}

// GET /api/dashboard/monthly-kpis?year=YYYY
//
// Returns per-month KPIs for the requested year (defaults to current year).
// Revenue is returned as Netto (MwSt 19% deducted; stored Brutto in DB).
// Closing rate logic mirrors GetDashboardKPIs: new-customer processes only,
// won bucketed by completed_at, losses by updated_at/follow_up_date/created_at.
//
// Response: array of 12 objects ordered Jan–Dec:
//
//	{
//	  "month":        1,        // 1–12
//	  "revenue":      1234.56,  // Netto sum of contracts starting this month
//	  "closed_deals": 3,        // won new-customer sales processes
//	  "closing_rate": 75.0      // won/decided * 100, null if no decisions
//	}
func (h *Handler) GetMonthlyKPIs(w http.ResponseWriter, r *http.Request) {
	yearStr := r.URL.Query().Get("year")
	year := time.Now().Year()
	if yearStr != "" {
		if _, err := fmt.Sscanf(yearStr, "%d", &year); err != nil || year < 2000 || year > 2100 {
			http.Error(w, "invalid year", http.StatusBadRequest)
			return
		}
	}

	type MonthlyKPI struct {
		Month       int      `json:"month"`
		Revenue     float64  `json:"revenue"`
		ClosedDeals int      `json:"closed_deals"`
		ClosingRate *float64 `json:"closing_rate"`
	}

	// Pre-fill all 12 months so the response always has 12 rows.
	rows := make([]MonthlyKPI, 12)
	for i := range rows {
		rows[i].Month = i + 1
	}

	// ── 1. Revenue per month (Brutto; converted below) ──────────────────────
	revenueRows, err := h.DB.Query(`
		SELECT
			EXTRACT(MONTH FROM c.start_date)::int AS month,
			COALESCE(SUM(c.revenue_total), 0)     AS revenue
		FROM contracts c
		JOIN clients cl ON cl.id = c.client_id
		WHERE EXTRACT(YEAR FROM c.start_date) = $1
		  AND cl.status = 'active'
		GROUP BY month
		ORDER BY month
	`, year)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer revenueRows.Close()
	for revenueRows.Next() {
		var m int
		var rev float64
		if err := revenueRows.Scan(&m, &rev); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if m >= 1 && m <= 12 {
			rows[m-1].Revenue = rev
		}
	}
	if err := revenueRows.Err(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Convert Brutto → Netto
	const mwst = 1.19
	for i := range rows {
		rows[i].Revenue /= mwst
	}

	// ── 2. Won new-customer deals per month (by completed_at) ───────────────
	wonRows, err := h.DB.Query(`
		SELECT
			EXTRACT(MONTH FROM cl.completed_at)::int AS month,
			COUNT(*)                                   AS won_count
		FROM sales_process sp
		JOIN clients cl ON cl.id = sp.client_id
		WHERE sp.closed = true
		  AND sp.follow_up_date IS NOT NULL
		  AND COALESCE(sp.is_imported_placeholder, false) = false
		  AND EXTRACT(YEAR FROM cl.completed_at) = $1
		  AND NOT EXISTS (
			SELECT 1 FROM contract_upsells cu
			WHERE cu.client_id = sp.client_id
			  AND cu.upsell_date IS NOT NULL
			  AND cu.upsell_date < sp.follow_up_date
		  )
		GROUP BY month
		ORDER BY month
	`, year)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer wonRows.Close()
	wonByMonth := make([]int, 12)
	for wonRows.Next() {
		var m, cnt int
		if err := wonRows.Scan(&m, &cnt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if m >= 1 && m <= 12 {
			wonByMonth[m-1] = cnt
		}
	}
	if err := wonRows.Err(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// ── 3. Decided new-customer deals per month ─────────────────────────────
	decidedRows, err := h.DB.Query(`
		SELECT
			EXTRACT(MONTH FROM
				CASE WHEN sp.closed = true
					THEN cl.completed_at::date
					ELSE COALESCE(sp.updated_at, sp.follow_up_date, sp.created_at)::date
				END
			)::int AS month,
			COUNT(*) AS decided_count
		FROM sales_process sp
		JOIN clients cl ON cl.id = sp.client_id
		WHERE (sp.closed = true OR (sp.closed = false AND sp.follow_up_result = true))
		  AND sp.follow_up_date IS NOT NULL
		  AND COALESCE(sp.is_imported_placeholder, false) = false
		  AND EXTRACT(YEAR FROM
			CASE WHEN sp.closed = true
				THEN cl.completed_at::date
				ELSE COALESCE(sp.updated_at, sp.follow_up_date, sp.created_at)::date
			END
		  ) = $1
		  AND NOT EXISTS (
			SELECT 1 FROM contract_upsells cu
			WHERE cu.client_id = sp.client_id
			  AND cu.upsell_date IS NOT NULL
			  AND cu.upsell_date < sp.follow_up_date
		  )
		GROUP BY month
		ORDER BY month
	`, year)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer decidedRows.Close()
	decidedByMonth := make([]int, 12)
	for decidedRows.Next() {
		var m, cnt int
		if err := decidedRows.Scan(&m, &cnt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if m >= 1 && m <= 12 {
			decidedByMonth[m-1] = cnt
		}
	}
	if err := decidedRows.Err(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// ── 4. Merge into result ─────────────────────────────────────────────────
	for i := range rows {
		rows[i].ClosedDeals = wonByMonth[i]
		if decidedByMonth[i] > 0 {
			rate := math.Round(float64(wonByMonth[i])/float64(decidedByMonth[i])*1000) / 10
			rows[i].ClosingRate = &rate
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rows)
}
