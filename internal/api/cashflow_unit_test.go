package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/domain"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/store"
)

// ── ListCashflowEntries ───────────────────────────────────────────────────────

func TestListCashflowEntries_InvalidStartDate(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := httptest.NewRequest(http.MethodGet, "/api/cashflow/entries?start_date=not-a-date", nil)
	w := httptest.NewRecorder()
	h.ListCashflowEntries(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestListCashflowEntries_InvalidEndDate(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := httptest.NewRequest(http.MethodGet, "/api/cashflow/entries?end_date=not-a-date", nil)
	w := httptest.NewRecorder()
	h.ListCashflowEntries(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestListCashflowEntries_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		listCashflowEntries: func(_ store.CashflowEntryFilter) ([]domain.CashflowEntry, int, error) {
			return nil, 0, errors.New("db down")
		},
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/cashflow/entries", nil)
	w := httptest.NewRecorder()
	h.ListCashflowEntries(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestListCashflowEntries_Success(t *testing.T) {
	due := "2026-06-01"
	h := &Handler{store: &mockStore{
		listCashflowEntries: func(_ store.CashflowEntryFilter) ([]domain.CashflowEntry, int, error) {
			return []domain.CashflowEntry{
				{ID: 1, ContractID: 10, DueDate: &due, Amount: 100.0, Status: "confirmed"},
			}, 1, nil
		},
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/cashflow/entries", nil)
	w := httptest.NewRecorder()
	h.ListCashflowEntries(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if resp["monetary_mode"] != monetaryModeBrutto {
		t.Fatalf("unexpected monetary_mode: %v", resp["monetary_mode"])
	}
	meta := resp["meta"].(map[string]interface{})
	if meta["total"].(float64) != 1 {
		t.Fatalf("unexpected total: %v", meta["total"])
	}
}

func TestListCashflowEntries_PerPageCappedAt500(t *testing.T) {
	var capturedFilter store.CashflowEntryFilter
	h := &Handler{store: &mockStore{
		listCashflowEntries: func(f store.CashflowEntryFilter) ([]domain.CashflowEntry, int, error) {
			capturedFilter = f
			return nil, 0, nil
		},
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/cashflow/entries?per_page=9999", nil)
	w := httptest.NewRecorder()
	h.ListCashflowEntries(w, req)
	if capturedFilter.PerPage != 500 {
		t.Fatalf("expected per_page capped at 500, got %d", capturedFilter.PerPage)
	}
}

func TestListCashflowEntries_SortOrderPassedToFilter(t *testing.T) {
	cases := []struct {
		param string
		want  string
	}{
		{"desc", "desc"},
		{"asc", "asc"},
		{"random", ""},  // invalid value ignored
		{"", ""},        // absent ignored
	}
	for _, tc := range cases {
		t.Run("sort_order="+tc.param, func(t *testing.T) {
			var captured store.CashflowEntryFilter
			h := &Handler{store: &mockStore{
				listCashflowEntries: func(f store.CashflowEntryFilter) ([]domain.CashflowEntry, int, error) {
					captured = f
					return nil, 0, nil
				},
			}}
			url := "/api/cashflow/entries"
			if tc.param != "" {
				url += "?sort_order=" + tc.param
			}
			req := httptest.NewRequest(http.MethodGet, url, nil)
			w := httptest.NewRecorder()
			h.ListCashflowEntries(w, req)
			if captured.SortOrder != tc.want {
				t.Fatalf("sort_order=%q: expected filter.SortOrder=%q, got %q", tc.param, tc.want, captured.SortOrder)
			}
		})
	}
}

func TestListCashflowEntries_ContractLabelInResponse(t *testing.T) {
	due := "2026-06-01"
	h := &Handler{store: &mockStore{
		listCashflowEntries: func(_ store.CashflowEntryFilter) ([]domain.CashflowEntry, int, error) {
			return []domain.CashflowEntry{
				{ID: 1, ContractID: 10, ContractLabel: "Alice Example", DueDate: &due, Amount: 100.0, Status: "confirmed"},
			}, 1, nil
		},
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/cashflow/entries", nil)
	w := httptest.NewRecorder()
	h.ListCashflowEntries(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Data []struct {
			ContractLabel string `json:"contract_label"`
		} `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if len(resp.Data) != 1 || resp.Data[0].ContractLabel != "Alice Example" {
		t.Fatalf("expected contract_label='Alice Example', got %+v", resp.Data)
	}
}

// ── CashflowForecast ──────────────────────────────────────────────────────────

func TestCashflowForecast_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		cashflowForecast: func(_ time.Time, _ time.Time, _ float64, _ *int) ([]domain.CashflowForecastRow, error) {
			return nil, errors.New("db down")
		},
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/cashflow/forecast", nil)
	w := httptest.NewRecorder()
	h.CashflowForecast(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestCashflowForecast_Success(t *testing.T) {
	h := &Handler{store: &mockStore{
		cashflowForecast: func(_ time.Time, _ time.Time, _ float64, _ *int) ([]domain.CashflowForecastRow, error) {
			return []domain.CashflowForecastRow{
				{Month: "2026-06", Confirmed: 500.0, Potential: 200.0},
			}, nil
		},
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/cashflow/forecast", nil)
	w := httptest.NewRecorder()
	h.CashflowForecast(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var rows []CashflowRow
	if err := json.NewDecoder(w.Body).Decode(&rows); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if len(rows) != 1 || rows[0].Month != "2026-06" {
		t.Fatalf("unexpected rows: %+v", rows)
	}
}

// ── UpdateCashflowEntryStatus ─────────────────────────────────────────────────

func chiReqWithID(method, url, id string, body []byte) *http.Request {
	req := httptest.NewRequest(method, url, bytes.NewReader(body))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestUpdateCashflowEntryStatus_InvalidID(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := chiReqWithID(http.MethodPatch, "/api/cashflow/entries/abc/status", "abc", []byte(`{"status":"confirmed"}`))
	w := httptest.NewRecorder()
	h.UpdateCashflowEntryStatus(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpdateCashflowEntryStatus_BadJSON(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := chiReqWithID(http.MethodPatch, "/api/cashflow/entries/1/status", "1", []byte("{bad"))
	w := httptest.NewRecorder()
	h.UpdateCashflowEntryStatus(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpdateCashflowEntryStatus_InvalidStatus(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := chiReqWithID(http.MethodPatch, "/api/cashflow/entries/1/status", "1", []byte(`{"status":"paid"}`))
	w := httptest.NewRecorder()
	h.UpdateCashflowEntryStatus(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpdateCashflowEntryStatus_NotFound(t *testing.T) {
	h := &Handler{store: &mockStore{
		updateCashflowEntryStatus: func(_ int, _ string) error {
			return store.ErrNotFound
		},
	}}
	req := chiReqWithID(http.MethodPatch, "/api/cashflow/entries/99/status", "99", []byte(`{"status":"confirmed"}`))
	w := httptest.NewRecorder()
	h.UpdateCashflowEntryStatus(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestUpdateCashflowEntryStatus_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		updateCashflowEntryStatus: func(_ int, _ string) error {
			return errors.New("db down")
		},
	}}
	req := chiReqWithID(http.MethodPatch, "/api/cashflow/entries/1/status", "1", []byte(`{"status":"confirmed"}`))
	w := httptest.NewRecorder()
	h.UpdateCashflowEntryStatus(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestUpdateCashflowEntryStatus_Success(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := chiReqWithID(http.MethodPatch, "/api/cashflow/entries/1/status", "1", []byte(`{"status":"confirmed"}`))
	w := httptest.NewRecorder()
	h.UpdateCashflowEntryStatus(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

// ── CashflowMetrics ───────────────────────────────────────────────────────────

func TestCashflowMetrics_YTDError(t *testing.T) {
	h := &Handler{store: &mockStore{
		cashflowYTDPaid: func(_, _ time.Time) (float64, error) {
			return 0, errors.New("db down")
		},
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/cashflow/metrics", nil)
	w := httptest.NewRecorder()
	h.CashflowMetrics(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestCashflowMetrics_NextMonthsError(t *testing.T) {
	h := &Handler{store: &mockStore{
		cashflowNextMonthsConfirmed: func(_, _ time.Time) ([]domain.MonthConfirmed, error) {
			return nil, errors.New("db down")
		},
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/cashflow/metrics", nil)
	w := httptest.NewRecorder()
	h.CashflowMetrics(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestCashflowMetrics_Success(t *testing.T) {
	h := &Handler{store: &mockStore{
		cashflowYTDPaid: func(_, _ time.Time) (float64, error) {
			return 1200.0, nil
		},
		cashflowNextMonthsConfirmed: func(_, _ time.Time) ([]domain.MonthConfirmed, error) {
			return []domain.MonthConfirmed{
				{Month: "2026-06", Confirmed: 400.0},
				{Month: "2026-07", Confirmed: 600.0},
			}, nil
		},
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/cashflow/metrics", nil)
	w := httptest.NewRecorder()
	h.CashflowMetrics(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if resp["ytd_paid_amount"].(float64) != 1200.0 {
		t.Fatalf("unexpected ytd_paid_amount: %v", resp["ytd_paid_amount"])
	}
	if resp["avg_confirmed_next3"].(float64) != 500.0 {
		t.Fatalf("expected avg_confirmed_next3=500, got %v", resp["avg_confirmed_next3"])
	}
}
