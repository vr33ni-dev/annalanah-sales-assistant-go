package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-chi/chi/v5"
)

func TestCreateContract_InvalidPaymentFreq(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	h := &Handler{DB: db}

	c := Contract{ClientID: 1, SalesProcessID: intPtr(2), StartDate: "2025-01-01", DurationMonths: 12, RevenueTotal: 1200, PaymentFreq: "invalid"}
	b, _ := json.Marshal(c)
	req := httptest.NewRequest(http.MethodPost, "/api/contracts", bytes.NewReader(b))
	w := httptest.NewRecorder()

	h.CreateContract(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateContract_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}

	c := Contract{
		ClientID:       1,
		SalesProcessID: intPtr(2),
		StartDate:      "2025-01-01",
		DurationMonths: 0, // allow 0 duration for testing, test cashflow entires generation in integration test
		RevenueTotal:   1200,
		PaymentFreq:    "monthly",
	}

	b, _ := json.Marshal(c)
	req := httptest.NewRequest(http.MethodPost, "/api/contracts", bytes.NewReader(b))
	w := httptest.NewRecorder()

	mock.ExpectBegin()

	created := time.Now()
	rows := sqlmock.NewRows([]string{"id", "created_at"}).
		AddRow(7, created)

	expectedStart, _ := time.Parse("2006-01-02", c.StartDate)
	expectedEnd := expectedStart.AddDate(0, c.DurationMonths, 0)

	mock.ExpectQuery("INSERT INTO contracts").
		WithArgs(
			c.ClientID,
			c.SalesProcessID,
			expectedStart,
			expectedEnd,
			c.DurationMonths,
			c.RevenueTotal,
			c.PaymentFreq,
		).
		WillReturnRows(rows)

	mock.ExpectCommit()

	h.CreateContract(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestUpdateContract_InvalidStartDate(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	h := &Handler{DB: db}

	reqBody := UpdateContractRequest{StartDate: "bad-date", DurationMonths: 12, RevenueTotal: 1000, PaymentFreq: "monthly"}
	b, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPatch, "/api/contracts/1", bytes.NewReader(b))
	// set chi route param
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()

	h.UpdateContract(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateContract_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	h := &Handler{DB: db}

	reqBody := UpdateContractRequest{StartDate: "2025-01-01", DurationMonths: 12, RevenueTotal: 1000, PaymentFreq: "monthly"}
	b, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPatch, "/api/contracts/5", bytes.NewReader(b))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "5")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	// Expect transaction begin
	mock.ExpectBegin()
	// Expect update
	mock.ExpectExec("UPDATE contracts").WithArgs(
		sqlmock.AnyArg(), // start_date
		sqlmock.AnyArg(), // end_date
		reqBody.DurationMonths,
		reqBody.RevenueTotal,
		reqBody.PaymentFreq,
		5,
	).WillReturnResult(sqlmock.NewResult(0, 1))
	// Expect delete
	mock.ExpectExec("DELETE FROM cashflow_entries").WithArgs(5).WillReturnResult(sqlmock.NewResult(0, 1))
	// Expect insertCashflowEntriesTx: there will be one INSERT per period
	expectedStart, _ := time.Parse("2006-01-02", reqBody.StartDate)
	expectedEnd := expectedStart.AddDate(0, reqBody.DurationMonths, 0)
	periods := 0
	cur := expectedStart
	for !cur.After(expectedEnd) {
		periods++
		cur = cur.AddDate(0, 1, 0)
	}
	for i := 0; i < periods; i++ {
		mock.ExpectExec("INSERT INTO cashflow_entries").WillReturnResult(sqlmock.NewResult(0, 1))
	}
	// Expect commit
	mock.ExpectCommit()

	h.UpdateContract(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// Helper function to get pointer to int
func intPtr(i int) *int {
	return &i
}
