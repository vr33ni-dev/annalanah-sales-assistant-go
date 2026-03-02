package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestStartSalesProcess_RequiresInitialContactDate(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}

	h := &Handler{DB: db}

	// missing InitialContactDate
	reqBody := StartSalesProcessRequest{
		Name:  "Alice",
		Email: "a@example.com",
	}
	b, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/sales/start", bytes.NewReader(b))
	w := httptest.NewRecorder()

	h.StartSalesProcess(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestStartSalesProcess_ExistingClientWithActiveContract_ReturnsConflict(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}

	h := &Handler{DB: db}

	// prepare request with ClientID
	initial := "2025-10-01"
	clientID := 42
	reqBody := StartSalesProcessRequest{
		Name:               "Bob",
		Email:              "b@example.com",
		InitialContactDate: &initial,
		ClientID:           &clientID,
	}
	b, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/sales/start", bytes.NewReader(b))
	w := httptest.NewRecorder()

	// Expect the lookup of existing client (SELECT name, phone, source)
	rows := sqlmock.NewRows([]string{"name", "phone", "source"}).AddRow("Bob", nil, nil)
	mock.ExpectQuery(`SELECT name, phone, source`).WithArgs(clientID).WillReturnRows(rows)

	// Expect the active contract EXISTS query -> return true
	existRows := sqlmock.NewRows([]string{"exists"}).AddRow(true)
	mock.ExpectQuery(`SELECT EXISTS`).WithArgs(clientID).WillReturnRows(existRows)

	h.StartSalesProcess(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func TestListSalesProcesses_WithComments(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}

	// sales_process query -> two rows
	now := time.Now()
	salesCols := []string{"id", "client_id", "client_name", "client_email", "client_phone", "client_source", "completed_at", "stage", "created_at", "initial_contact_date", "follow_up_date", "follow_up_result", "closed", "revenue", "stage_id", "lead_id"}
	salesRows := sqlmock.NewRows(salesCols).
		AddRow(1, 10, "Client A", sql.NullString{String: "a@x", Valid: true}, sql.NullString{String: "111", Valid: true}, sql.NullString{String: "web", Valid: true}, sql.NullTime{Time: now, Valid: true}, "follow_up", now, nil, nil, sql.NullBool{Bool: false, Valid: true}, sql.NullBool{Bool: false, Valid: true}, sql.NullFloat64{Float64: 0, Valid: false}, sql.NullInt64{Int64: 0, Valid: false}, sql.NullInt64{Int64: 0, Valid: false}).
		AddRow(2, 11, "Client B", sql.NullString{Valid: false}, sql.NullString{Valid: false}, sql.NullString{String: "ref", Valid: true}, sql.NullTime{Valid: false}, "initial_contact", now, nil, nil, sql.NullBool{Valid: false}, sql.NullBool{Valid: false}, sql.NullFloat64{Valid: false}, sql.NullInt64{Valid: false}, sql.NullInt64{Valid: false})

	mock.ExpectQuery("FROM sales_process sp").WillReturnRows(salesRows)

	// batch comments query for both processes — match actual selected columns
	commentRows := sqlmock.NewRows([]string{"id", "entity_id", "author", "body", "metadata", "created_at", "updated_at"}).
		AddRow(100, int64(1), sql.NullString{String: "Sam", Valid: true}, "note1", `{"x":1}`, now, now).
		AddRow(101, int64(2), sql.NullString{Valid: false}, "note2", nil, now, now)
	mock.ExpectQuery("FROM comments").WithArgs(sqlmock.AnyArg()).WillReturnRows(commentRows)

	req := httptest.NewRequest("GET", "/api/sales", nil)
	w := httptest.NewRecorder()
	h.ListSalesProcesses(w, req)

	if w.Result().StatusCode != 200 {
		t.Fatalf("expected 200, got %d; body=%s", w.Result().StatusCode, w.Body.String())
	}

	var resp []SalesProcessResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp) != 2 {
		t.Fatalf("expected 2 processes, got %d", len(resp))
	}
	if len(resp[0].Comments) != 1 || len(resp[1].Comments) != 1 {
		t.Fatalf("expected comments attached to both processes; resp=%#v", resp)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestListUpsellCategories_DateRangeFiltersQuery(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}

	start := "2026-01-01"
	end := "2026-01-31"
	startT, _ := time.Parse("2006-01-02", start)
	endT, _ := time.Parse("2006-01-02", end)

	cols := []string{
		"id",
		"sales_process_id",
		"client_id",
		"upsell_date",
		"upsell_result",
		"upsell_revenue",
		"previous_contract_id",
		"new_contract_id",
		"created_at",
		"updated_at",
		"contract_start_date",
		"contract_duration_months",
		"contract_frequency",
	}
	rows := sqlmock.NewRows(cols).
		AddRow(1, 10, 100, "2026-01-10", "verlaengerung", 123.45, nil, nil, "2026-01-10T10:00:00Z", "2026-01-10T10:00:00Z", nil, nil, nil)

	mock.ExpectQuery("FROM contract_upsells cu").WithArgs(startT, endT).WillReturnRows(rows)

	req := httptest.NewRequest("GET", "/api/upsells/categories?start_date="+start+"&end_date="+end, nil)
	w := httptest.NewRecorder()
	h.ListUpsellCategories(w, req)

	if w.Result().StatusCode != 200 {
		t.Fatalf("expected 200, got %d; body=%s", w.Result().StatusCode, w.Body.String())
	}

	var resp map[string][]ContractUpsell
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp["successful"]) != 1 {
		t.Fatalf("expected 1 successful upsell, got %#v", resp)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestGetUpsellAnalytics_DateRangeFiltersQuery(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}

	start := "2026-02-01"
	end := "2026-02-28"
	startT, _ := time.Parse("2006-01-02", start)
	endT, _ := time.Parse("2006-01-02", end)

	// query row result columns correspond to Scan order in handler
	result := sqlmock.NewRows([]string{"verlangerung_count", "keine_verlangerung_count", "scheduled_count", "verlangerungsquote", "umsatz_sum"}).
		AddRow(2, 1, 3, 66.7, 1000.0)

	mock.ExpectQuery("FROM contract_upsells cu").WithArgs(startT, endT).WillReturnRows(result)

	req := httptest.NewRequest("GET", "/api/upsells/analytics?start_date="+start+"&end_date="+end, nil)
	w := httptest.NewRecorder()
	h.GetUpsellAnalytics(w, req)

	if w.Result().StatusCode != 200 {
		t.Fatalf("expected 200, got %d; body=%s", w.Result().StatusCode, w.Body.String())
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["verlangerung_count"] != float64(2) {
		t.Fatalf("expected verlangerung_count=2, got %#v", body["verlangerung_count"])
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
