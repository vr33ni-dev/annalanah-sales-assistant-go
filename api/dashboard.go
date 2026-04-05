package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"
)

// GET /api/dashboard/kpis?start_date=YYYY-MM-DD&end_date=YYYY-MM-DD
//
// Returns all KPIs needed by the Dashboard page so the frontend does not have
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
		SELECT COUNT(*), COALESCE(SUM(c.revenue_total), 0)
		FROM contracts c
		JOIN clients cl ON cl.id = c.client_id
		WHERE (c.end_date IS NULL OR c.end_date >= CURRENT_DATE)
		  AND cl.status = 'active'
		  AND c.id NOT IN (
			SELECT previous_contract_id FROM contract_upsells
			WHERE previous_contract_id IS NOT NULL
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

	var closingRateNew *float64
	if decidedNewCount > 0 {
		v := float64(int(float64(wonNewCount)/float64(decidedNewCount)*1000+0.5)) / 10
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
