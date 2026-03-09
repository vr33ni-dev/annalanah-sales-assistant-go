package api

import (
	"bytes"
	"context"
	"database/sql"
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

func TestGetContract_Handler(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}
	now := time.Now()

	mainRow := sqlmock.NewRows([]string{
		"id", "client_id", "client_name", "sales_process_id",
		"start_date", "end_date", "created_at", "updated_at", "duration_months", "revenue_total", "payment_frequency",
		"base_monthly_amount", "next_due_date",
	}).AddRow(
		118, 5, "Acme GmbH", 44,
		now, now.AddDate(0, 6, 0), now, now, 6, 1200.0, "monthly",
		200.0, now.AddDate(0, 1, 0),
	)

	mock.ExpectQuery("WITH overdue AS").WithArgs(118).WillReturnRows(mainRow)

	commentRows := sqlmock.NewRows([]string{"id", "entity_id", "author", "body", "metadata", "created_at", "updated_at"}).
		AddRow(77, 118, "tester", "note", `{"k":"v"}`, now, now)
	mock.ExpectQuery("FROM comments").WithArgs(118).WillReturnRows(commentRows)

	cashflowRows := sqlmock.NewRows([]string{"id", "contract_id", "due_date", "amount", "status", "updated_at"}).
		AddRow(88, 118, now, 123.45, "pending", now)
	mock.ExpectQuery("FROM cashflow_entries").WithArgs(118).WillReturnRows(cashflowRows)

	req := httptest.NewRequest(http.MethodGet, "/api/contracts/118", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "118")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	h.GetContract(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var out ContractResponse
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if out.ID != 118 {
		t.Fatalf("expected id 118, got %d", out.ID)
	}
	if len(out.Comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(out.Comments))
	}
	if len(out.Cashflow) != 1 {
		t.Fatalf("expected 1 cashflow entry, got %d", len(out.Cashflow))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestGetContract_InvalidID(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/api/contracts/nope", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "nope")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	h.GetContract(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestGetContract_NotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}
	mock.ExpectQuery("WITH overdue AS").WithArgs(999).WillReturnError(sql.ErrNoRows)

	req := httptest.NewRequest(http.MethodGet, "/api/contracts/999", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "999")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	h.GetContract(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
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
	for {
		next := cur.AddDate(0, 1, 0)
		if next.After(expectedEnd) {
			break
		}
		periods++
		cur = next
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

	// Oct 1 to Dec 1 -> two full monthly periods: Oct->Nov, Nov->Dec
	start := time.Date(2025, 10, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC)

	// Expect two Exec calls for monthly periods
	for i := 0; i < 2; i++ {
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
	for {
		next := addMonthClamped(cur, 2)
		if next.After(expectedEnd) {
			break
		}
		periods++
		cur = next
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
	for {
		next := addMonthClamped(cur, 2)
		if next.After(end) {
			break
		}
		periods++
		cur = next
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
	for {
		next := addMonthClamped(cur, 3)
		if next.After(end) {
			break
		}
		periods++
		cur = next
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

func TestNormalizePaymentFrequency(t *testing.T) {
	got, err := normalizePaymentFrequency("  MONTHLY  ", 6)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got != "monthly" {
		t.Fatalf("expected monthly, got %q", got)
	}

	if _, err := normalizePaymentFrequency("bi-yearly", 6); err == nil {
		t.Fatalf("expected bi-yearly validation error")
	}

	if _, err := normalizePaymentFrequency("weekly", 12); err == nil {
		t.Fatalf("expected invalid frequency error")
	}
}

func TestInsertCashflowEntriesTx_EndBeforeStart(t *testing.T) {
	start := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	err := insertCashflowEntriesTx(nil, 1, start, end, 100, "monthly")
	if err == nil {
		t.Fatalf("expected error when endDate < startDate")
	}
}

func TestInsertCashflowEntriesTx_NoFullPeriods_NoInsert(t *testing.T) {
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

	start := time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC)

	if err := insertCashflowEntriesTx(tx, 5, start, end, 200, "monthly"); err != nil {
		t.Fatalf("insertCashflowEntriesTx failed: %v", err)
	}

	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback tx: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestListContracts_Handler_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}

	mainRows := sqlmock.NewRows([]string{
		"id", "client_id", "client_name", "sales_process_id",
		"start_date", "end_date", "created_at", "duration_months", "revenue_total", "payment_frequency",
		"base_monthly_amount", "next_due_date",
	}).AddRow(
		1, 10, "Acme GmbH", nil,
		"2025-01-01", nil, nil, 12, 1200.0, "monthly",
		100.0, nil,
	)

	mock.ExpectQuery("WITH overdue AS").WillReturnRows(mainRows)

	now := time.Now()
	commentRows := sqlmock.NewRows([]string{"id", "entity_id", "author", "body", "metadata", "created_at", "updated_at"}).
		AddRow(77, 1, "tester", "note", `{"k":"v"}`, now, now)
	mock.ExpectQuery("FROM comments").WillReturnRows(commentRows)

	cashflowRows := sqlmock.NewRows([]string{"id", "contract_id", "due_date", "amount", "status", "updated_at"}).
		AddRow(88, 1, now, 123.45, "pending", now)
	mock.ExpectQuery("FROM cashflow_entries").WillReturnRows(cashflowRows)

	req := httptest.NewRequest(http.MethodGet, "/api/contracts", nil)
	w := httptest.NewRecorder()

	h.ListContracts(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var out []ContractResponse
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if len(out) != 1 {
		t.Fatalf("expected 1 contract, got %d", len(out))
	}
	if out[0].ClientName != "Acme GmbH" {
		t.Fatalf("unexpected client name: %q", out[0].ClientName)
	}
	if len(out[0].Comments) != 1 {
		t.Fatalf("expected 1 comment by default, got %d", len(out[0].Comments))
	}
	if len(out[0].Cashflow) != 1 {
		t.Fatalf("expected 1 cashflow entry by default, got %d", len(out[0].Cashflow))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestListContracts_Handler_CompactOmitsRelations(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}

	mainRows := sqlmock.NewRows([]string{
		"id", "client_id", "client_name", "sales_process_id",
		"start_date", "end_date", "created_at", "duration_months", "revenue_total", "payment_frequency",
		"base_monthly_amount", "next_due_date",
	}).AddRow(
		1, 10, "Acme GmbH", nil,
		"2025-01-01", nil, nil, 12, 1200.0, "monthly",
		100.0, nil,
	)

	mock.ExpectQuery("WITH overdue AS").WillReturnRows(mainRows)

	req := httptest.NewRequest(http.MethodGet, "/api/contracts?compact=true", nil)
	w := httptest.NewRecorder()

	h.ListContracts(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var out []ContractResponse
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if len(out) != 1 {
		t.Fatalf("expected 1 contract, got %d", len(out))
	}
	if out[0].Comments != nil {
		t.Fatalf("expected comments to be omitted in compact mode, got %d entries", len(out[0].Comments))
	}
	if out[0].Cashflow != nil {
		t.Fatalf("expected cashflow to be omitted in compact mode, got %d entries", len(out[0].Cashflow))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestListContracts_Handler_IncludeExpired(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}

	mainRows := sqlmock.NewRows([]string{
		"id", "client_id", "client_name", "sales_process_id",
		"start_date", "end_date", "created_at", "duration_months", "revenue_total", "payment_frequency",
		"base_monthly_amount", "next_due_date",
	}).AddRow(
		2, 11, "Expired Co", nil,
		"2024-01-01", "2025-01-31", nil, 12, 1200.0, "monthly",
		100.0, nil,
	)

	mock.ExpectQuery("WITH overdue AS").WillReturnRows(mainRows)

	req := httptest.NewRequest(http.MethodGet, "/api/contracts?include_expired=true", nil)
	w := httptest.NewRecorder()

	h.ListContracts(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var out []ContractResponse
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if len(out) != 1 {
		t.Fatalf("expected 1 contract, got %d", len(out))
	}
	if out[0].ClientName != "Expired Co" {
		t.Fatalf("unexpected client name: %q", out[0].ClientName)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestListContractCashflowEntries_Handler(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}

	now := time.Now()
	rows := sqlmock.NewRows([]string{"id", "contract_id", "due_date", "amount", "status", "updated_at"}).
		AddRow(1, 7, now, 123.45, "pending", now)

	mock.ExpectQuery("SELECT id, contract_id, due_date, amount, status, updated_at").
		WithArgs(7).
		WillReturnRows(rows)

	req := httptest.NewRequest(http.MethodGet, "/api/contracts/7/cashflow", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "7")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	h.ListContractCashflowEntries(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var out []map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 row, got %d", len(out))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestListContractCashflowEntries_InvalidID(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/api/contracts/nope/cashflow", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "nope")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	h.ListContractCashflowEntries(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
