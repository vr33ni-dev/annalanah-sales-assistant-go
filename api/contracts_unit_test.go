package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-chi/chi/v5"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/pkg/mailer"
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

func TestCreateContract_TriggersMailer(t *testing.T) {
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
		DurationMonths: 0,
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
		).WillReturnRows(rows)

	mock.ExpectCommit()

	// stub mailer
	orig := mailer.SendMailFunc
	defer func() { mailer.SendMailFunc = orig }()

	ch := make(chan struct{}, 1)
	mailer.SendMailFunc = func(to, subject, body string) error {
		ch <- struct{}{}
		return nil
	}

	// set env var to trigger notification
	os.Setenv("NEW_CONTRACT_NOTIFY_EMAIL", "ops@example.com")
	defer os.Unsetenv("NEW_CONTRACT_NOTIFY_EMAIL")

	h.CreateContract(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	select {
	case <-ch:
		// success
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timed out waiting for mailer to be called")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestCreateContract_NoNotifyEnv_NoMailerCalled(t *testing.T) {
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
		DurationMonths: 0,
		RevenueTotal:   1200,
		PaymentFreq:    "monthly",
	}

	b, _ := json.Marshal(c)
	req := httptest.NewRequest(http.MethodPost, "/api/contracts", bytes.NewReader(b))
	w := httptest.NewRecorder()

	mock.ExpectBegin()

	created := time.Now()
	rows := sqlmock.NewRows([]string{"id", "created_at"}).AddRow(7, created)

	expectedStart, _ := time.Parse("2006-01-02", c.StartDate)
	expectedEnd := expectedStart.AddDate(0, c.DurationMonths, 0)

	mock.ExpectQuery("INSERT INTO contracts").WithArgs(
		c.ClientID,
		c.SalesProcessID,
		expectedStart,
		expectedEnd,
		c.DurationMonths,
		c.RevenueTotal,
		c.PaymentFreq,
	).WillReturnRows(rows)

	mock.ExpectCommit()

	// stub mailer to fail the test if invoked
	orig := mailer.SendMailFunc
	defer func() { mailer.SendMailFunc = orig }()
	mailer.SendMailFunc = func(to, subject, body string) error {
		t.Fatalf("unexpected mailer call when NEW_CONTRACT_NOTIFY_EMAIL unset")
		return nil
	}

	// ensure env var is unset
	os.Unsetenv("NEW_CONTRACT_NOTIFY_EMAIL")

	h.CreateContract(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestCreateContract_MailerReturnsError_HandlerStillSucceeds(t *testing.T) {
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
		DurationMonths: 0,
		RevenueTotal:   1200,
		PaymentFreq:    "monthly",
	}

	b, _ := json.Marshal(c)
	req := httptest.NewRequest(http.MethodPost, "/api/contracts", bytes.NewReader(b))
	w := httptest.NewRecorder()

	mock.ExpectBegin()

	created := time.Now()
	rows := sqlmock.NewRows([]string{"id", "created_at"}).AddRow(7, created)

	expectedStart, _ := time.Parse("2006-01-02", c.StartDate)
	expectedEnd := expectedStart.AddDate(0, c.DurationMonths, 0)

	mock.ExpectQuery("INSERT INTO contracts").WithArgs(
		c.ClientID,
		c.SalesProcessID,
		expectedStart,
		expectedEnd,
		c.DurationMonths,
		c.RevenueTotal,
		c.PaymentFreq,
	).WillReturnRows(rows)

	mock.ExpectCommit()

	// stub mailer to return error but signal via channel
	orig := mailer.SendMailFunc
	defer func() { mailer.SendMailFunc = orig }()

	ch := make(chan struct{}, 1)
	mailer.SendMailFunc = func(to, subject, body string) error {
		ch <- struct{}{}
		return errors.New("smtp fail")
	}

	// set env var to trigger notification
	os.Setenv("NEW_CONTRACT_NOTIFY_EMAIL", "ops@example.com")
	defer os.Unsetenv("NEW_CONTRACT_NOTIFY_EMAIL")

	h.CreateContract(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	select {
	case <-ch:
		// mailer was called; handler should still succeed
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timed out waiting for mailer to be called")
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

func TestInsertCashflowEntriesTx_Monthly(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}

	// Oct 1 to Dec 1 inclusive -> 3 periods
	start := time.Date(2025, 10, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC)

	// Expect three Exec calls for monthly periods
	for i := 0; i < 3; i++ {
		mock.ExpectExec("INSERT INTO cashflow_entries").WillReturnResult(sqlmock.NewResult(1, 1))
	}

	if err := insertCashflowEntriesTx(tx, 42, start, end, 300.0, "monthly"); err != nil {
		t.Fatalf("insertCashflowEntriesTx failed: %v", err)
	}

	mock.ExpectCommit()
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit tx: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestUpdateContract_RecreateSchedule_BiMonthly(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	h := &Handler{DB: db}

	reqBody := UpdateContractRequest{StartDate: "2025-01-01", DurationMonths: 6, RevenueTotal: 1200, PaymentFreq: "bi-monthly"}
	b, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPatch, "/api/contracts/9", bytes.NewReader(b))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "9")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	// Begin
	mock.ExpectBegin()
	// Update
	mock.ExpectExec("UPDATE contracts").WithArgs(
		sqlmock.AnyArg(), // start_date
		sqlmock.AnyArg(), // end_date
		reqBody.DurationMonths,
		reqBody.RevenueTotal,
		reqBody.PaymentFreq,
		9,
	).WillReturnResult(sqlmock.NewResult(0, 1))
	// Delete
	mock.ExpectExec("DELETE FROM cashflow_entries").WithArgs(9).WillReturnResult(sqlmock.NewResult(0, 1))

	// Compute expected periods for bi-monthly over 6 months
	expectedStart, _ := time.Parse("2006-01-02", reqBody.StartDate)
	expectedEnd := expectedStart.AddDate(0, reqBody.DurationMonths, 0)
	periods := 0
	cur := expectedStart
	for !cur.After(expectedEnd) {
		periods++
		cur = addMonthClamped(cur, 2)
	}

	for i := 0; i < periods; i++ {
		mock.ExpectExec("INSERT INTO cashflow_entries").WillReturnResult(sqlmock.NewResult(0, 1))
	}

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

func TestInsertCashflowEntriesTx_BiMonthly(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC)

	periods := 0
	cur := start
	for !cur.After(end) {
		periods++
		cur = addMonthClamped(cur, 2)
	}

	for i := 0; i < periods; i++ {
		mock.ExpectExec("INSERT INTO cashflow_entries").WillReturnResult(sqlmock.NewResult(1, 1))
	}

	if err := insertCashflowEntriesTx(tx, 99, start, end, 500.0, "bi-monthly"); err != nil {
		t.Fatalf("insertCashflowEntriesTx failed: %v", err)
	}

	mock.ExpectCommit()
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit tx: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestInsertCashflowEntriesTx_Quarterly(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 10, 1, 0, 0, 0, 0, time.UTC)

	periods := 0
	cur := start
	for !cur.After(end) {
		periods++
		cur = addMonthClamped(cur, 3)
	}

	for i := 0; i < periods; i++ {
		mock.ExpectExec("INSERT INTO cashflow_entries").WillReturnResult(sqlmock.NewResult(1, 1))
	}

	if err := insertCashflowEntriesTx(tx, 100, start, end, 1500.0, "quarterly"); err != nil {
		t.Fatalf("insertCashflowEntriesTx failed: %v", err)
	}

	mock.ExpectCommit()
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit tx: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestInsertCashflowEntriesTx_OneTime(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}

	start := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)
	end := start

	mock.ExpectExec("INSERT INTO cashflow_entries").WillReturnResult(sqlmock.NewResult(1, 1))

	if err := insertCashflowEntriesTx(tx, 101, start, end, 999.0, "one-time"); err != nil {
		t.Fatalf("insertCashflowEntriesTx failed: %v", err)
	}

	mock.ExpectCommit()
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit tx: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestUpdateContract_UpdateExecFails(t *testing.T) {
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

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE contracts").WillReturnError(errors.New("update failed"))
	mock.ExpectRollback()

	h.UpdateContract(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
