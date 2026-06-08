package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/domain"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/store"
)

// ── normalizePaymentFrequency ─────────────────────────────────────────────────

func TestNormalizePaymentFrequency_Invalid(t *testing.T) {
	_, err := normalizePaymentFrequency("weekly", 12)
	if err == nil {
		t.Fatal("expected error for invalid payment frequency")
	}
}

func TestNormalizePaymentFrequency_BiYearlyTooShort(t *testing.T) {
	_, err := normalizePaymentFrequency("bi-yearly", 6)
	if err == nil {
		t.Fatal("expected error for bi-yearly with duration < 12")
	}
}

func TestNormalizePaymentFrequency_NormalizesCase(t *testing.T) {
	pf, err := normalizePaymentFrequency("  Monthly  ", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pf != "monthly" {
		t.Fatalf("expected 'monthly', got %q", pf)
	}
}

func TestNormalizePaymentFrequency_ValidFrequencies(t *testing.T) {
	for _, freq := range []string{"monthly", "bi-monthly", "quarterly", "one-time", "bi-yearly"} {
		months := 1
		if freq == "bi-yearly" {
			months = 12
		}
		if _, err := normalizePaymentFrequency(freq, months); err != nil {
			t.Errorf("expected %q to be valid, got err: %v", freq, err)
		}
	}
}

// ── ListContracts ─────────────────────────────────────────────────────────────

func TestListContracts_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		listContracts: func(_ context.Context, _, _, _ bool) ([]domain.ContractRow, error) {
			return nil, errors.New("db down")
		},
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/contracts", nil)
	w := httptest.NewRecorder()
	h.ListContracts(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestListContracts_Success(t *testing.T) {
	h := &Handler{store: &mockStore{
		listContracts: func(_ context.Context, _, _, _ bool) ([]domain.ContractRow, error) {
			return []domain.ContractRow{
				{ID: 1, ClientID: 10, ClientName: "Alice", StartDate: "2026-01-01", RevenueBrutto: 1190.0, PaymentFreq: "monthly"},
			}, nil
		},
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/contracts", nil)
	w := httptest.NewRecorder()
	h.ListContracts(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var out []ContractResponse
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != 1 || out[0].RevenueTotal != 1000.0 {
		t.Fatalf("expected 1 contract with revenue_total=1000, got %+v", out)
	}
}

// ── GetContract ───────────────────────────────────────────────────────────────

func TestGetContract_InvalidID(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := chiReqWithID(http.MethodGet, "/api/contracts/abc", "abc", nil)
	w := httptest.NewRecorder()
	h.GetContract(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestGetContract_NotFound(t *testing.T) {
	h := &Handler{store: &mockStore{
		getContractByID: func(_ context.Context, _ int) (domain.ContractRow, error) {
			return domain.ContractRow{}, sql.ErrNoRows
		},
	}}
	req := chiReqWithID(http.MethodGet, "/api/contracts/99", "99", nil)
	w := httptest.NewRecorder()
	h.GetContract(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestGetContract_Success(t *testing.T) {
	h := &Handler{store: &mockStore{
		getContractByID: func(_ context.Context, _ int) (domain.ContractRow, error) {
			return domain.ContractRow{ID: 1, ClientID: 10, ClientName: "Alice", StartDate: "2026-01-01", RevenueBrutto: 2380.0, PaymentFreq: "monthly"}, nil
		},
	}}
	req := chiReqWithID(http.MethodGet, "/api/contracts/1", "1", nil)
	w := httptest.NewRecorder()
	h.GetContract(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var out ContractResponse
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.RevenueTotal != 2000.0 {
		t.Fatalf("expected revenue_total=2000, got %v", out.RevenueTotal)
	}
}

// -- PauseContract ───────────────────────────────────────────────────────────────

func TestPauseContract_InvalidID(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := chiReqWithID(http.MethodPatch, "/api/contracts/abc/pause", "abc", []byte(`{}`))
	w := httptest.NewRecorder()
	h.PauseContract(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestPauseContract_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		pauseContract: func(_ context.Context, _ int, _, _ string) error {
			return errors.New("db down")
		},
	}}
	b := []byte(`{"new_end_date":"2026-12-31","reason":"Client requested pause"}`)
	req := chiReqWithID(http.MethodPatch, "/api/contracts/1/pause", "1", b)
	w := httptest.NewRecorder()
	h.PauseContract(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestPauseContract_Success(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	b := []byte(`{"new_end_date":"2026-12-31","reason":"Client requested pause"}`)
	req := chiReqWithID(http.MethodPatch, "/api/contracts/1/pause", "1", b)
	w := httptest.NewRecorder()
	h.PauseContract(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

func TestPauseContract_BadJSON(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := chiReqWithID(http.MethodPatch, "/api/contracts/1/pause", "1", []byte(`{bad`))
	w := httptest.NewRecorder()
	h.PauseContract(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestPauseContract_MissingEndDate(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := chiReqWithID(http.MethodPatch, "/api/contracts/1/pause", "1", []byte(`{"reason":"pause requested"}`))
	w := httptest.NewRecorder()
	h.PauseContract(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestPauseContract_MissingReason(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := chiReqWithID(http.MethodPatch, "/api/contracts/1/pause", "1", []byte(`{"new_end_date":"2026-12-31"}`))
	w := httptest.NewRecorder()
	h.PauseContract(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestPauseContract_InvalidDateFormat(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	b := []byte(`{"new_end_date":"31-12-2026","reason":"pause requested"}`)
	req := chiReqWithID(http.MethodPatch, "/api/contracts/1/pause", "1", b)
	w := httptest.NewRecorder()
	h.PauseContract(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestPauseContract_NotFound(t *testing.T) {
	h := &Handler{store: &mockStore{
		pauseContract: func(_ context.Context, _ int, _, _ string) error {
			return errors.New("contract not found")
		},
	}}
	b := []byte(`{"new_end_date":"2026-12-31","reason":"pause requested"}`)
	req := chiReqWithID(http.MethodPatch, "/api/contracts/1/pause", "1", b)
	w := httptest.NewRecorder()
	h.PauseContract(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestPauseContract_DateBeforeStart(t *testing.T) {
	h := &Handler{store: &mockStore{
		pauseContract: func(_ context.Context, _ int, _, _ string) error {
			return errors.New("new_end_date cannot be before start_date")
		},
	}}
	b := []byte(`{"new_end_date":"2026-12-31","reason":"pause requested"}`)
	req := chiReqWithID(http.MethodPatch, "/api/contracts/1/pause", "1", b)
	w := httptest.NewRecorder()
	h.PauseContract(w, req)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", w.Code)
	}
}

// ── ListContractCashflowEntries ───────────────────────────────────────────────

func TestListContractCashflowEntries_InvalidID(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := chiReqWithID(http.MethodGet, "/api/contracts/abc/cashflow", "abc", nil)
	w := httptest.NewRecorder()
	h.ListContractCashflowEntries(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestListContractCashflowEntries_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		getContractCashflow: func(_ context.Context, _ int) ([]domain.CashflowEntry, error) {
			return nil, errors.New("db down")
		},
	}}
	req := chiReqWithID(http.MethodGet, "/api/contracts/1/cashflow", "1", nil)
	w := httptest.NewRecorder()
	h.ListContractCashflowEntries(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestListContractCashflowEntries_Success(t *testing.T) {
	due := "2026-02-01"
	h := &Handler{store: &mockStore{
		getContractCashflow: func(_ context.Context, _ int) ([]domain.CashflowEntry, error) {
			return []domain.CashflowEntry{
				{ID: 1, ContractID: 1, DueDate: &due, Amount: 100.0, Status: "confirmed"},
			}, nil
		},
	}}
	req := chiReqWithID(http.MethodGet, "/api/contracts/1/cashflow", "1", nil)
	w := httptest.NewRecorder()
	h.ListContractCashflowEntries(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var out []ContractCashflowEntryResponse
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != 1 || out[0].Status != "confirmed" {
		t.Fatalf("unexpected response: %+v", out)
	}
}

// ── CreateContract ────────────────────────────────────────────────────────────

func TestCreateContract_BadJSON(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := httptest.NewRequest(http.MethodPost, "/api/contracts", bytes.NewReader([]byte("{bad")))
	w := httptest.NewRecorder()
	h.CreateContract(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCreateContract_InvalidPaymentFreq(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	b, _ := json.Marshal(domain.Contract{ClientID: 1, StartDate: "2026-01-01", DurationMonths: 12, RevenueTotal: 1200, PaymentFreq: "weekly"})
	req := httptest.NewRequest(http.MethodPost, "/api/contracts", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.CreateContract(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCreateContract_InvalidStartDate(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	b, _ := json.Marshal(domain.Contract{ClientID: 1, StartDate: "not-a-date", DurationMonths: 1, RevenueTotal: 100, PaymentFreq: "monthly"})
	req := httptest.NewRequest(http.MethodPost, "/api/contracts", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.CreateContract(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCreateContract_EndDateBeforeStart(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	endDate := "2025-12-31"
	b, _ := json.Marshal(domain.Contract{ClientID: 1, StartDate: "2026-01-01", EndDate: &endDate, DurationMonths: 1, RevenueTotal: 100, PaymentFreq: "monthly"})
	req := httptest.NewRequest(http.MethodPost, "/api/contracts", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.CreateContract(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCreateContract_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		createContract: func(_ context.Context, _ store.ContractCreateInput) (int, *string, error) {
			return 0, nil, errors.New("db down")
		},
	}}
	b, _ := json.Marshal(domain.Contract{ClientID: 1, StartDate: "2026-01-01", DurationMonths: 1, RevenueTotal: 100, PaymentFreq: "monthly"})
	req := httptest.NewRequest(http.MethodPost, "/api/contracts", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.CreateContract(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestCreateContract_Success(t *testing.T) {
	created := "2026-01-01T00:00:00Z"
	h := &Handler{store: &mockStore{
		createContract: func(_ context.Context, _ store.ContractCreateInput) (int, *string, error) {
			return 7, &created, nil
		},
	}}
	b, _ := json.Marshal(domain.Contract{ClientID: 1, StartDate: "2026-01-01", DurationMonths: 12, RevenueTotal: 1200, PaymentFreq: "monthly"})
	req := httptest.NewRequest(http.MethodPost, "/api/contracts", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.CreateContract(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var out domain.Contract
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.ID != 7 {
		t.Fatalf("expected id=7, got %d", out.ID)
	}
}

// ── UpdateContract ────────────────────────────────────────────────────────────

func TestUpdateContract_InvalidID(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := chiReqWithID(http.MethodPatch, "/api/contracts/abc", "abc", []byte(`{}`))
	w := httptest.NewRecorder()
	h.UpdateContract(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpdateContract_InvalidPaymentFreq(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	b, _ := json.Marshal(UpdateContractRequest{StartDate: "2026-01-01", DurationMonths: 12, RevenueTotal: 1200, PaymentFreq: "weekly"})
	req := chiReqWithID(http.MethodPatch, "/api/contracts/1", "1", b)
	w := httptest.NewRecorder()
	h.UpdateContract(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpdateContract_InvalidStartDate(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	b, _ := json.Marshal(UpdateContractRequest{StartDate: "not-a-date", DurationMonths: 1, RevenueTotal: 100, PaymentFreq: "monthly"})
	req := chiReqWithID(http.MethodPatch, "/api/contracts/1", "1", b)
	w := httptest.NewRecorder()
	h.UpdateContract(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpdateContract_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		updateContract: func(_ context.Context, _ int, _, _ time.Time, _ int, _ float64, _ string) error {
			return errors.New("db down")
		},
	}}
	b, _ := json.Marshal(UpdateContractRequest{StartDate: "2026-01-01", DurationMonths: 1, RevenueTotal: 100, PaymentFreq: "monthly"})
	req := chiReqWithID(http.MethodPatch, "/api/contracts/1", "1", b)
	w := httptest.NewRecorder()
	h.UpdateContract(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestUpdateContract_Success(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	b, _ := json.Marshal(UpdateContractRequest{StartDate: "2026-01-01", DurationMonths: 12, RevenueTotal: 1200, PaymentFreq: "monthly"})
	req := chiReqWithID(http.MethodPatch, "/api/contracts/1", "1", b)
	w := httptest.NewRecorder()
	h.UpdateContract(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}
