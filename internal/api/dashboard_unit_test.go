package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/domain"
)

// ── GetContractsInRange ───────────────────────────────────────────────────────

func TestGetContractsInRange_InvalidType(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/contracts-in-range?type=foo", nil)
	w := httptest.NewRecorder()
	h.GetContractsInRange(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestGetContractsInRange_InvalidStartDate(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/contracts-in-range?type=neukunden&start_date=invalid", nil)
	w := httptest.NewRecorder()
	h.GetContractsInRange(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestGetContractsInRange_InvalidEndDate(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/contracts-in-range?type=neukunden&end_date=31-12-2025", nil)
	w := httptest.NewRecorder()
	h.GetContractsInRange(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestGetContractsInRange_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		getContractsInRange: func(_ context.Context, _ string, _, _ *time.Time) ([]domain.ContractSummary, error) {
			return nil, errors.New("db down")
		},
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/contracts-in-range?type=neukunden", nil)
	w := httptest.NewRecorder()
	h.GetContractsInRange(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestGetContractsInRange_Success(t *testing.T) {
	endDate := "2026-04-10"
	h := &Handler{store: &mockStore{
		getContractsInRange: func(_ context.Context, _ string, _, _ *time.Time) ([]domain.ContractSummary, error) {
			return []domain.ContractSummary{
				{ContractID: 101, ClientID: 11, ClientName: "Alice", StartDate: "2026-01-10", EndDate: &endDate, RevenueBrutto: 1190.0},
				{ContractID: 102, ClientID: 12, ClientName: "Bob", StartDate: "2026-02-01", RevenueBrutto: 2380.0},
			}, nil
		},
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/contracts-in-range?type=neukunden&start_date=2026-01-01&end_date=2026-12-31", nil)
	w := httptest.NewRecorder()
	h.GetContractsInRange(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var out []map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(out))
	}
	if out[0]["revenue_netto"].(float64) != 1000.0 {
		t.Fatalf("expected revenue_netto=1000, got %v", out[0]["revenue_netto"])
	}
	if out[0]["monetary_mode"] != monetaryModeNetto {
		t.Fatalf("expected monetary_mode=netto, got %v", out[0]["monetary_mode"])
	}
	if _, hasEndDate := out[1]["end_date"]; hasEndDate {
		t.Fatalf("expected end_date omitted when nil")
	}
}

// ── GetDashboardKPIs ──────────────────────────────────────────────────────────

func TestGetDashboardKPIs_InvalidStartDate(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/kpis?start_date=not-a-date", nil)
	w := httptest.NewRecorder()
	h.GetDashboardKPIs(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestGetDashboardKPIs_InvalidEndDate(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/kpis?end_date=13-99-2025", nil)
	w := httptest.NewRecorder()
	h.GetDashboardKPIs(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestGetDashboardKPIs_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		getDashboardKPIs: func(_ context.Context, _, _ *time.Time) (domain.DashboardKPIsRaw, error) {
			return domain.DashboardKPIsRaw{}, errors.New("db down")
		},
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/kpis", nil)
	w := httptest.NewRecorder()
	h.GetDashboardKPIs(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestGetDashboardKPIs_Success(t *testing.T) {
	verl := 60.0
	h := &Handler{store: &mockStore{
		getDashboardKPIs: func(_ context.Context, _, _ *time.Time) (domain.DashboardKPIsRaw, error) {
			return domain.DashboardKPIsRaw{
				TotalRevenueBrutto:      4760.0,  // 4000 netto
				ActiveRevenueBrutto:     4760.0,
				CLVActiveClientsBrutto:  9520.0,  // 8000 netto
				GesamtCLVBrutto:         11900.0, // 10000 netto
				ActiveContractsCount:    4,
				WonNewCount:             3,
				DecidedNewCount:         5,
				VerlaengerungCount:      2,
				KeineVerlaengerungCount: 1,
				Verlaengerungsquote:     &verl,
			}, nil
		},
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/kpis?start_date=2026-01-01&end_date=2026-12-31", nil)
	w := httptest.NewRecorder()
	h.GetDashboardKPIs(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var kpis map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&kpis); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, key := range []string{
		"monetary_mode", "total_revenue", "active_revenue",
		"clv_active_clients", "clv_all_time", "active_contracts_count",
		"won_new_count", "decided_new_count", "closing_rate_new",
		"verlaengerung_count", "keine_verlaengerung_count",
	} {
		if _, ok := kpis[key]; !ok {
			t.Errorf("missing key %q in response", key)
		}
	}
	if kpis["monetary_mode"] != monetaryModeNetto {
		t.Errorf("expected monetary_mode=netto")
	}
	// closing_rate_new = 3/5 * 100 = 60.0
	if cr := kpis["closing_rate_new"].(float64); cr != 60.0 {
		t.Errorf("expected closing_rate_new=60, got %v", cr)
	}
}

// ── GetMonthlyKPIs ────────────────────────────────────────────────────────────

func TestGetMonthlyKPIs_InvalidYear(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/monthly-kpis?year=invalid", nil)
	w := httptest.NewRecorder()
	h.GetMonthlyKPIs(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestGetMonthlyKPIs_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		getMonthlyKPIs: func(_ context.Context, _ int) ([]domain.MonthlyKPIRaw, error) {
			return nil, errors.New("db down")
		},
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/monthly-kpis?year=2026", nil)
	w := httptest.NewRecorder()
	h.GetMonthlyKPIs(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestGetMonthlyKPIs_Success(t *testing.T) {
	h := &Handler{store: &mockStore{
		getMonthlyKPIs: func(_ context.Context, _ int) ([]domain.MonthlyKPIRaw, error) {
			return []domain.MonthlyKPIRaw{
				{Month: 1, Revenue: 1190.0, Won: 3, Decided: 4},
				{Month: 2, Revenue: 2380.0, Won: 0, Decided: 0},
			}, nil
		},
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/monthly-kpis?year=2026", nil)
	w := httptest.NewRecorder()
	h.GetMonthlyKPIs(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var out []map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(out))
	}
	// Jan: 1190/1.19 = 1000, closing_rate = 3/4*100 = 75
	if out[0]["revenue"].(float64) != 1000.0 {
		t.Errorf("expected jan revenue=1000, got %v", out[0]["revenue"])
	}
	if cr := out[0]["closing_rate"].(float64); cr != 75.0 {
		t.Errorf("expected jan closing_rate=75, got %v", cr)
	}
	if out[0]["monetary_mode"] != monetaryModeNetto {
		t.Errorf("expected monetary_mode=netto")
	}
	// Feb: no decided deals → closing_rate absent or null
	if v := out[1]["closing_rate"]; v != nil {
		t.Errorf("expected nil closing_rate for month with no decided deals, got %v", v)
	}
}
