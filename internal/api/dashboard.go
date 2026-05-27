package api

import (
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

	var start, end *time.Time
	if startStr != "" {
		t, err := time.Parse("2006-01-02", startStr)
		if err != nil {
			writeJSONError(w, "invalid start_date (expected YYYY-MM-DD)", http.StatusBadRequest)
			return
		}
		start = &t
	}
	if endStr != "" {
		t, err := time.Parse("2006-01-02", endStr)
		if err != nil {
			writeJSONError(w, "invalid end_date (expected YYYY-MM-DD)", http.StatusBadRequest)
			return
		}
		end = &t
	}

	summaries, err := h.store.GetContractsInRange(r.Context(), typ, start, end)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	mwstRate := defaultMwstRate

	type ContractRow struct {
		ContractID   int     `json:"contract_id"`
		ClientID     int     `json:"client_id"`
		ClientName   string  `json:"client_name"`
		StartDate    string  `json:"start_date"`
		EndDate      *string `json:"end_date,omitempty"`
		RevenueNetto float64 `json:"revenue_netto"`
		MonetaryMode string  `json:"monetary_mode"`
	}

	out := make([]ContractRow, len(summaries))
	for i, s := range summaries {
		out[i] = ContractRow{
			ContractID:   s.ContractID,
			ClientID:     s.ClientID,
			ClientName:   s.ClientName,
			StartDate:    s.StartDate,
			EndDate:      s.EndDate,
			RevenueNetto: netFromGross(s.RevenueBrutto, mwstRate),
			MonetaryMode: monetaryModeNetto,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (h *Handler) GetDashboardKPIs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	var start, end *time.Time
	if v := q.Get("start_date"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			http.Error(w, "invalid start_date (expected YYYY-MM-DD)", http.StatusBadRequest)
			return
		}
		start = &t
	}
	if v := q.Get("end_date"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			http.Error(w, "invalid end_date (expected YYYY-MM-DD)", http.StatusBadRequest)
			return
		}
		end = &t
	}

	kpis, err := h.store.GetDashboardKPIs(r.Context(), start, end)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	mwstRate := defaultMwstRate
	renewalRevenue := netFromGross(kpis.RenewalRevenueBrutto, mwstRate)
	newCustomerRevenue := netFromGross(kpis.NewCustomerRevenueBrutto, mwstRate)
	totalRevenue := netFromGross(kpis.TotalRevenueBrutto, mwstRate)
	totalCLV := netFromGross(kpis.CLVActiveClientsBrutto, mwstRate)
	gesamtCLV := netFromGross(kpis.GesamtCLVBrutto, mwstRate)
	activeRevenue := netFromGross(kpis.ActiveRevenueBrutto, mwstRate)

	var closingRateNew *float64
	if kpis.DecidedNewCount > 0 {
		v := math.Round(float64(kpis.WonNewCount)/float64(kpis.DecidedNewCount)*1000) / 10
		closingRateNew = &v
	}

	var avgVertragswert, avgCLVPerContract *float64
	if kpis.ActiveContractsCount > 0 {
		v1 := activeRevenue / float64(kpis.ActiveContractsCount)
		v2 := totalCLV / float64(kpis.ActiveContractsCount)
		avgVertragswert = &v1
		avgCLVPerContract = &v2
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"monetary_mode":             monetaryModeNetto,
		"renewal_revenue":           renewalRevenue,
		"new_customer_revenue":      newCustomerRevenue,
		"total_revenue":             totalRevenue,
		"active_revenue":            activeRevenue,
		"clv_active_clients":        totalCLV,
		"clv_all_time":              gesamtCLV,
		"avg_vertragswert":          avgVertragswert,
		"avg_clv_per_contract":      avgCLVPerContract,
		"active_contracts_count":    kpis.ActiveContractsCount,
		"won_new_count":             kpis.WonNewCount,
		"decided_new_count":         kpis.DecidedNewCount,
		"closing_rate_new":          closingRateNew,
		"verlaengerungsquote":       kpis.Verlaengerungsquote,
		"verlaengerung_count":       kpis.VerlaengerungCount,
		"keine_verlaengerung_count": kpis.KeineVerlaengerungCount,
	})
}

func (h *Handler) GetMonthlyKPIs(w http.ResponseWriter, r *http.Request) {
	yearStr := r.URL.Query().Get("year")
	year := time.Now().Year()
	if yearStr != "" {
		if _, err := fmt.Sscanf(yearStr, "%d", &year); err != nil || year < 2000 || year > 2100 {
			http.Error(w, "invalid year", http.StatusBadRequest)
			return
		}
	}

	rawKPIs, err := h.store.GetMonthlyKPIs(r.Context(), year)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	type MonthlyKPI struct {
		Month        int      `json:"month"`
		Revenue      float64  `json:"revenue"`
		ClosedDeals  int      `json:"closed_deals"`
		ClosingRate  *float64 `json:"closing_rate"`
		MonetaryMode string   `json:"monetary_mode"`
	}

	mwstRate := defaultMwstRate
	out := make([]MonthlyKPI, len(rawKPIs))
	for i, raw := range rawKPIs {
		kpi := MonthlyKPI{
			Month:        raw.Month,
			Revenue:      netFromGross(raw.Revenue, mwstRate),
			ClosedDeals:  raw.Won,
			MonetaryMode: monetaryModeNetto,
		}
		if raw.Decided > 0 {
			rate := math.Round(float64(raw.Won)/float64(raw.Decided)*1000) / 10
			kpi.ClosingRate = &rate
		}
		out[i] = kpi
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
