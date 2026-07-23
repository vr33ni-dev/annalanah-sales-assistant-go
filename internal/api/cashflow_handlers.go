// cashflow.go — HTTP handlers for cashflow: list entries, forecast, metrics, and status updates.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/store"
)

const defaultMwstRate = 1.19

const (
	monetaryModeNetto  = "netto"
	monetaryModeBrutto = "brutto"
)

type CashflowEntryResponse struct {
	ID            int     `json:"id"`
	ContractID    int     `json:"contract_id"`
	ContractLabel string  `json:"contract_label,omitempty"`
	DueDate       *string `json:"due_date"`
	Amount        float64 `json:"amount"`
	Status        string  `json:"status"`
	UpdatedAt     *string `json:"updated_at,omitempty"`
	MonetaryMode  string  `json:"monetary_mode"`
}

type CashflowRow struct {
	Month        string  `json:"month"`
	Confirmed    float64 `json:"confirmed"`
	Potential    float64 `json:"potential"`
	MonetaryMode string  `json:"monetary_mode"`
}

// GET /api/cashflow/entries
func (h *Handler) ListCashflowEntries(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.CashflowEntryFilter{
		ContractID: q.Get("contract_id"),
		ClientID:   q.Get("client_id"),
		Status:     q.Get("status"),
		Page:       1,
		PerPage:    50,
	}
	if v := q.Get("start_date"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			http.Error(w, "invalid start_date (expected YYYY-MM-DD)", http.StatusBadRequest)
			return
		}
		f.StartDate = &t
	}
	if v := q.Get("end_date"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			http.Error(w, "invalid end_date (expected YYYY-MM-DD)", http.StatusBadRequest)
			return
		}
		f.EndDate = &t
	}
	if v := q.Get("page"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 {
			f.Page = p
		}
	}
	if v := q.Get("per_page"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 {
			if p > 500 {
				f.PerPage = 500
			} else {
				f.PerPage = p
			}
		}
	}
	if v := q.Get("sort_order"); v == "desc" || v == "asc" {
		f.SortOrder = v
	}

	entries, total, err := h.store.ListCashflowEntries(f)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	out := make([]CashflowEntryResponse, len(entries))
	for i, e := range entries {
		out[i] = CashflowEntryResponse{
			ID:            e.ID,
			ContractID:    e.ContractID,
			ContractLabel: e.ContractLabel,
			DueDate:       e.DueDate,
			Amount:        e.Amount,
			Status:        e.Status,
			UpdatedAt:     e.UpdatedAt,
			MonetaryMode:  monetaryModeBrutto,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"monetary_mode": monetaryModeBrutto,
		"data":          out,
		"meta": map[string]interface{}{
			"total":    total,
			"page":     f.Page,
			"per_page": f.PerPage,
		},
	})
}

// GET /api/cashflow/forecast
func (h *Handler) CashflowForecast(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	displayMonths := 6
	if ms := r.URL.Query().Get("months"); ms != "" {
		if m, err := strconv.Atoi(ms); err == nil && m > 0 {
			displayMonths = m
		}
	}
	end := start.AddDate(0, displayMonths, 0)

	var contractID *int
	if cidStr := r.URL.Query().Get("contract_id"); cidStr != "" {
		if cid, err := strconv.Atoi(cidStr); err == nil {
			contractID = &cid
		}
	}

	avgRevenuePerContract := h.getNumericSetting("avg_revenue_per_contract", 0)

	rows, err := h.store.CashflowForecast(start, end, avgRevenuePerContract, contractID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	out := make([]CashflowRow, len(rows))
	for i, r := range rows {
		out[i] = CashflowRow{
			Month:        r.Month,
			Confirmed:    r.Confirmed,
			Potential:    r.Potential,
			MonetaryMode: monetaryModeBrutto,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// PATCH /api/cashflow/entries/{id}/status
func (h *Handler) UpdateCashflowEntryStatus(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	switch body.Status {
	case "confirmed", "overdue":
	default:
		http.Error(w, "invalid status: must be one of confirmed, overdue", http.StatusBadRequest)
		return
	}

	if err := h.store.UpdateCashflowEntryStatus(id, body.Status); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GET /api/cashflow/metrics
func (h *Handler) CashflowMetrics(w http.ResponseWriter, r *http.Request) {
	now := time.Now()

	ytdStart := time.Date(now.Year(), time.January, 1, 0, 0, 0, 0, time.UTC)
	if os.Getenv("DEBUG_DB") == "true" {
		fmt.Printf("CashflowMetrics: ytd params: start=%v end=%v\n", ytdStart, now)
	}

	ytdPaid, err := h.store.CashflowYTDPaid(ytdStart, now)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	monthsElapsed := int(now.Month())
	var avgMonthlyYtd float64
	if monthsElapsed > 0 {
		avgMonthlyYtd = ytdPaid / float64(monthsElapsed)
	}

	startMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	endMonth := startMonth.AddDate(0, 3, 0)
	if os.Getenv("DEBUG_DB") == "true" {
		fmt.Printf("CashflowMetrics: forecast params: start=%v end=%v\n", startMonth, endMonth)
	}

	list, err := h.store.CashflowNextMonthsConfirmed(startMonth, endMonth)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var sumNext3 float64
	for _, m := range list {
		sumNext3 += m.Confirmed
	}
	var avgConfirmedNext3 float64
	if len(list) > 0 {
		avgConfirmedNext3 = sumNext3 / float64(len(list))
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"monetary_mode":       monetaryModeBrutto,
		"avg_monthly_ytd":     avgMonthlyYtd,
		"months_elapsed_ytd":  monthsElapsed,
		"ytd_paid_amount":     ytdPaid,
		"confirmed_next3":     list,
		"avg_confirmed_next3": avgConfirmedNext3,
	})
}

func (h *Handler) GetNumericSettingForTest(key string, def float64) float64 {
	return h.getNumericSetting(key, def)
}
