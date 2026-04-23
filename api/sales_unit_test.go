package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-chi/chi/v5"
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
	defer db.Close()

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

func TestListSalesProcesses_ExcludesImportedPlaceholders(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}

	salesRows := sqlmock.NewRows([]string{"id", "client_id", "client_name", "client_email", "client_phone", "client_source", "completed_at", "stage", "created_at", "initial_contact_date", "follow_up_date", "follow_up_result", "closed", "revenue", "stage_id", "lead_id"})
	mock.ExpectQuery(`WHERE COALESCE\(sp\.is_imported_placeholder, false\) = false`).WillReturnRows(salesRows)

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
	if len(resp) != 0 {
		t.Fatalf("expected 0 processes, got %d", len(resp))
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
	upsellDate, _ := time.Parse("2006-01-02", "2026-01-10")
	createdAt, _ := time.Parse(time.RFC3339, "2026-01-10T10:00:00Z")

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
		AddRow(1, 10, 100, upsellDate, "verlaengerung", 123.45, nil, nil, createdAt, createdAt, nil, nil, nil)

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
	result := sqlmock.NewRows([]string{"verlaengerung_count", "keine_verlaengerung_count", "scheduled_count", "verlaengerungsquote", "umsatz_sum"}).
		AddRow(2, 1, 3, 66.7, 1000.0)

	mock.ExpectQuery("FROM contract_upsells cu").WithArgs(startT, endT).WillReturnRows(result)

	// Second query: monthly breakdown (verlaengerung by month)
	monthlyRows := sqlmock.NewRows([]string{"month", "revenue"})
	mock.ExpectQuery("GROUP BY month").WithArgs(startT, endT).WillReturnRows(monthlyRows)

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
	if body["verlaengerung_count"] != float64(2) {
		t.Fatalf("expected verlaengerung_count=2, got %#v", body["verlaengerung_count"])
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestUpdateSalesProcess_NoShowForcesClosedFalseAndClearsCompletedAt(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}

	// Incoming request tries to close, but marks follow_up_result=false (no-show).
	// Handler should force closed=false and not require contract details.
	closed := true
	followUpResult := false
	reqBody := SalesProcessUpdateRequest{
		FollowUpResult: &followUpResult,
		Closed:         &closed,
	}
	b, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPatch, "/api/sales/1", bytes.NewReader(b))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	// 1) UPDATE sales_process
	mock.ExpectExec("UPDATE sales_process").
		WithArgs(nil, nil, false, false, nil, 1, false, nil).
		WillReturnResult(sqlmock.NewResult(0, 1))

	// 2) Load client_id for completed_at updates
	mock.ExpectQuery("SELECT client_id FROM sales_process WHERE id = ").
		WithArgs(1).
		WillReturnRows(sqlmock.NewRows([]string{"client_id"}).AddRow(10))

	// 3) Clear completed_at because closed=false
	mock.ExpectExec("UPDATE clients SET completed_at = NULL").
		WithArgs(10).
		WillReturnResult(sqlmock.NewResult(0, 1))

	// 4) Sync client status
	mock.ExpectExec("UPDATE clients c").
		WithArgs(1).
		WillReturnResult(sqlmock.NewResult(0, 1))

	// 5) Return updated row
	respCols := []string{
		"id",
		"client_id",
		"client_name",
		"client_email",
		"client_phone",
		"client_source",
		"completed_at",
		"stage",
		"initial_contact_date",
		"follow_up_date",
		"follow_up_result",
		"closed",
		"revenue",
		"stage_id",
	}
	respRows := sqlmock.NewRows(respCols).
		AddRow(
			1,
			10,
			"Client X",
			sql.NullString{Valid: false},
			sql.NullString{Valid: false},
			sql.NullString{String: "paid", Valid: true},
			sql.NullTime{Valid: false},
			"lost",
			nil,
			"2026-03-03",
			sql.NullBool{Bool: false, Valid: true},
			sql.NullBool{Bool: false, Valid: true},
			sql.NullFloat64{Valid: false},
			sql.NullInt64{Valid: false},
		)
	mock.ExpectQuery("FROM sales_process sp").WithArgs(1).WillReturnRows(respRows)

	// 6) Comments query (empty) — now queries by client_id=10
	commentRows := sqlmock.NewRows([]string{"id", "client_id", "entity_type", "entity_id", "author", "body", "metadata", "created_at", "updated_at"})
	mock.ExpectQuery("FROM comments").WithArgs(10).WillReturnRows(commentRows)

	w := httptest.NewRecorder()
	h.UpdateSalesProcess(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp SalesProcessResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Closed == nil || *resp.Closed != false {
		t.Fatalf("expected closed=false, got %#v", resp.Closed)
	}
	if resp.Stage != "lost" {
		t.Fatalf("expected stage=lost, got %q", resp.Stage)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestUpdateSalesProcess_WithStageID_UpdatesStageLink(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}

	stageID := 7
	reqBody := SalesProcessUpdateRequest{StageID: &stageID}
	b, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPatch, "/api/sales/1", bytes.NewReader(b))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	// 1) UPDATE sales_process includes $7 stage_id parameter.
	mock.ExpectExec("UPDATE sales_process").
		WithArgs(nil, nil, nil, nil, nil, 1, true, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	// 2) Resolve client_id for downstream sync.
	mock.ExpectQuery("SELECT client_id FROM sales_process WHERE id = ").
		WithArgs(1).
		WillReturnRows(sqlmock.NewRows([]string{"client_id"}).AddRow(10))

	// 3) Sync client status.
	mock.ExpectExec("UPDATE clients c").
		WithArgs(1).
		WillReturnResult(sqlmock.NewResult(0, 1))

	// 4) Return updated row with stage_id=7.
	respCols := []string{
		"id",
		"client_id",
		"client_name",
		"client_email",
		"client_phone",
		"client_source",
		"completed_at",
		"stage",
		"initial_contact_date",
		"follow_up_date",
		"follow_up_result",
		"closed",
		"revenue",
		"stage_id",
	}
	respRows := sqlmock.NewRows(respCols).
		AddRow(
			1,
			10,
			"Client X",
			sql.NullString{Valid: false},
			sql.NullString{Valid: false},
			sql.NullString{String: "paid", Valid: true},
			sql.NullTime{Valid: false},
			"follow_up",
			"2026-03-01",
			"2026-03-03",
			sql.NullBool{Valid: false},
			sql.NullBool{Valid: false},
			sql.NullFloat64{Valid: false},
			sql.NullInt64{Int64: 7, Valid: true},
		)
	mock.ExpectQuery("FROM sales_process sp").WithArgs(1).WillReturnRows(respRows)

	// 5) Comments query (empty) for client_id=10.
	commentRows := sqlmock.NewRows([]string{"id", "client_id", "entity_type", "entity_id", "author", "body", "metadata", "created_at", "updated_at"})
	mock.ExpectQuery("FROM comments").WithArgs(10).WillReturnRows(commentRows)

	w := httptest.NewRecorder()
	h.UpdateSalesProcess(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp SalesProcessResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.StageID == nil || *resp.StageID != 7 {
		t.Fatalf("expected stage_id=7, got %#v", resp.StageID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestUpdateSalesProcess_WithStageIDNull_ClearsStageLink(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}

	req := httptest.NewRequest(http.MethodPatch, "/api/sales/1", bytes.NewReader([]byte(`{"stage_id":null}`)))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	// 1) UPDATE sales_process explicitly clears stage_id via $8=NULL.
	mock.ExpectExec("UPDATE sales_process").
		WithArgs(nil, nil, nil, nil, nil, 1, true, nil).
		WillReturnResult(sqlmock.NewResult(0, 1))

	// 2) Resolve client_id for downstream sync.
	mock.ExpectQuery("SELECT client_id FROM sales_process WHERE id = ").
		WithArgs(1).
		WillReturnRows(sqlmock.NewRows([]string{"client_id"}).AddRow(10))

	// 3) Sync client status.
	mock.ExpectExec("UPDATE clients c").
		WithArgs(1).
		WillReturnResult(sqlmock.NewResult(0, 1))

	// 4) Return updated row with stage_id=NULL.
	respCols := []string{
		"id",
		"client_id",
		"client_name",
		"client_email",
		"client_phone",
		"client_source",
		"completed_at",
		"stage",
		"initial_contact_date",
		"follow_up_date",
		"follow_up_result",
		"closed",
		"revenue",
		"stage_id",
	}
	respRows := sqlmock.NewRows(respCols).
		AddRow(
			1,
			10,
			"Client X",
			sql.NullString{Valid: false},
			sql.NullString{Valid: false},
			sql.NullString{String: "paid", Valid: true},
			sql.NullTime{Valid: false},
			"follow_up",
			"2026-03-01",
			"2026-03-03",
			sql.NullBool{Valid: false},
			sql.NullBool{Valid: false},
			sql.NullFloat64{Valid: false},
			sql.NullInt64{Valid: false},
		)
	mock.ExpectQuery("FROM sales_process sp").WithArgs(1).WillReturnRows(respRows)

	// 5) Comments query (empty) for client_id=10.
	commentRows := sqlmock.NewRows([]string{"id", "client_id", "entity_type", "entity_id", "author", "body", "metadata", "created_at", "updated_at"})
	mock.ExpectQuery("FROM comments").WithArgs(10).WillReturnRows(commentRows)

	w := httptest.NewRecorder()
	h.UpdateSalesProcess(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp SalesProcessResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.StageID != nil {
		t.Fatalf("expected stage_id=nil after unlink, got %#v", resp.StageID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestUpdateSalesProcess_LostFromUnconvertedLead_DeletesTemporaryClient(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}

	followUpResult := false
	reqBody := SalesProcessUpdateRequest{FollowUpResult: &followUpResult}
	b, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPatch, "/api/sales/1", bytes.NewReader(b))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	// 1) UPDATE sales_process
	mock.ExpectExec("UPDATE sales_process").
		WithArgs(nil, nil, false, false, nil, 1, false, nil).
		WillReturnResult(sqlmock.NewResult(0, 1))

	// 2) client_id lookup for completed_at updates
	mock.ExpectQuery("SELECT client_id FROM sales_process WHERE id = ").WithArgs(1).
		WillReturnRows(sqlmock.NewRows([]string{"client_id"}).AddRow(10))

	// 3) Clear completed_at
	mock.ExpectExec("UPDATE clients SET completed_at = NULL").WithArgs(10).
		WillReturnResult(sqlmock.NewResult(0, 1))

	// 4) Sync client status
	mock.ExpectExec("UPDATE clients c").WithArgs(1).
		WillReturnResult(sqlmock.NewResult(0, 1))

	// 5) Return updated row (lost)
	respCols := []string{
		"id",
		"client_id",
		"client_name",
		"client_email",
		"client_phone",
		"client_source",
		"completed_at",
		"stage",
		"initial_contact_date",
		"follow_up_date",
		"follow_up_result",
		"closed",
		"revenue",
		"stage_id",
	}
	respRows := sqlmock.NewRows(respCols).
		AddRow(
			1,
			10,
			"Client X",
			sql.NullString{Valid: false},
			sql.NullString{Valid: false},
			sql.NullString{String: "paid", Valid: true},
			sql.NullTime{Valid: false},
			"lost",
			nil,
			"2026-03-03",
			sql.NullBool{Bool: false, Valid: true},
			sql.NullBool{Bool: false, Valid: true},
			sql.NullFloat64{Valid: false},
			sql.NullInt64{Valid: false},
		)
	mock.ExpectQuery("FROM sales_process sp").WithArgs(1).WillReturnRows(respRows)

	// 6) Comments query (empty) — now queries by client_id=10
	commentRows := sqlmock.NewRows([]string{"id", "client_id", "entity_type", "entity_id", "author", "body", "metadata", "created_at", "updated_at"})
	mock.ExpectQuery("FROM comments").WithArgs(10).WillReturnRows(commentRows)

	w := httptest.NewRecorder()
	h.UpdateSalesProcess(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// TestStartSalesProcess_NewClient_CreatesTrackerLeadAndReturnsLeadID verifies that
// when a brand-new client is created via POST /api/sales/start (no pre-existing lead,
// no pre-existing client), a tracker lead is inserted with converted=FALSE and
// converted_client_id set, and the response's SalesProcess.lead_id reflects it.
// converted=TRUE is only set when a contract is actually signed.
func TestStartSalesProcess_NewClient_CreatesTrackerLeadAndReturnsLeadID(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}

	initial := "2026-04-01"
	follow := "2026-04-10"
	reqBody := StartSalesProcessRequest{
		Name:               "Brand New Client",
		Email:              "brandnew@client.com",
		Phone:              "555123456",
		Source:             "organic",
		InitialContactDate: &initial,
		FollowUpDate:       &follow,
	}
	b, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/sales/start", bytes.NewReader(b))
	w := httptest.NewRecorder()

	// 1) resolveLeadForSalesStart: email lookup — no existing unconverted lead
	mock.ExpectQuery("SELECT id FROM leads").
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	// 2) runStartSalesProcessTx: begin transaction
	mock.ExpectBegin()

	// 3) Insert new client → id=10
	mock.ExpectQuery("INSERT INTO clients").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(10))

	// 4) Insert tracker lead for new client (converted=FALSE, converted_client_id set) → id=20
	mock.ExpectQuery("INSERT INTO leads").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(20))

	// 5) Backfill client status
	mock.ExpectExec("UPDATE clients").
		WillReturnResult(sqlmock.NewResult(0, 0))

	// 6) Insert sales_process → id=30
	mock.ExpectQuery("INSERT INTO sales_process").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(30))

	// 7) Commit
	mock.ExpectCommit()

	// 8) loadStartSalesProcessResponse: comments (empty)
	mock.ExpectQuery("FROM comments").
		WillReturnRows(sqlmock.NewRows([]string{"id", "client_id", "entity_type", "entity_id", "author", "body", "metadata", "created_at", "updated_at"}))

	// 9) loadStartSalesProcessResponse: client detail
	mock.ExpectQuery("FROM clients").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "email", "phone", "source", "source_stage_id"}).
			AddRow(10, "Brand New Client", "brandnew@client.com",
				sql.NullString{String: "555123456", Valid: true},
				sql.NullString{String: "organic", Valid: true},
				sql.NullInt64{Valid: false}))

	h.StartSalesProcess(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp StartSalesProcessResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.SalesProcess.LeadID == nil {
		t.Fatal("expected SalesProcess.LeadID to be non-nil for a newly created client")
	}
	if *resp.SalesProcess.LeadID != 20 {
		t.Fatalf("expected lead_id=20, got %d", *resp.SalesProcess.LeadID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// TestStartSalesProcess_NewClient_EmailExistsInLeads_LinksExistingLead verifies that
// when a new client is created with an email that already exists in the leads table,
// the leads INSERT conflicts (ON CONFLICT DO NOTHING → ErrNoRows from Scan), and the
// handler falls back to linking the pre-existing lead via email lookup.
func TestStartSalesProcess_NewClient_EmailExistsInLeads_LinksExistingLead(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}

	initial := "2026-04-01"
	reqBody := StartSalesProcessRequest{
		Name:               "Known Lead Client",
		Email:              "knownlead@example.com",
		InitialContactDate: &initial,
	}
	b, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/sales/start", bytes.NewReader(b))
	w := httptest.NewRecorder()

	// 1) resolveLeadForSalesStart: email lookup — no unconverted lead found
	mock.ExpectQuery("SELECT id FROM leads").
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	// 2) Begin transaction
	mock.ExpectBegin()

	// 3) Insert new client → id=11
	mock.ExpectQuery("INSERT INTO clients").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(11))

	// 4) Insert converted lead → ON CONFLICT (email): no row returned (ErrNoRows from Scan)
	mock.ExpectQuery("INSERT INTO leads").
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	// 5) Fallback: SELECT existing lead by email → id=7
	mock.ExpectQuery("SELECT id FROM leads WHERE LOWER").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(7))

	// 6) Backfill client status
	mock.ExpectExec("UPDATE clients").
		WillReturnResult(sqlmock.NewResult(0, 0))

	// 7) Insert sales_process → id=31
	mock.ExpectQuery("INSERT INTO sales_process").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(31))

	// 8) Commit
	mock.ExpectCommit()

	// 9) Comments (empty)
	mock.ExpectQuery("FROM comments").
		WillReturnRows(sqlmock.NewRows([]string{"id", "client_id", "entity_type", "entity_id", "author", "body", "metadata", "created_at", "updated_at"}))

	// 10) Client detail
	mock.ExpectQuery("FROM clients").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "email", "phone", "source", "source_stage_id"}).
			AddRow(11, "Known Lead Client", "knownlead@example.com",
				sql.NullString{Valid: false},
				sql.NullString{Valid: false},
				sql.NullInt64{Valid: false}))

	h.StartSalesProcess(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp StartSalesProcessResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.SalesProcess.LeadID == nil {
		t.Fatal("expected SalesProcess.LeadID to be non-nil when existing lead found by email")
	}
	if *resp.SalesProcess.LeadID != 7 {
		t.Fatalf("expected lead_id=7 (existing lead), got %d", *resp.SalesProcess.LeadID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// TestStartSalesProcess_PreExistingLead_NewClient_LinksLeadViaClientID verifies that when
// a pre-existing unconverted lead is matched by email and a new client is created,
// only converted_client_id is set on the lead — converted stays FALSE.
// converted=TRUE is only set when a contract is actually signed.
func TestStartSalesProcess_PreExistingLead_NewClient_LinksLeadViaClientID(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}

	initial := "2026-04-01"
	reqBody := StartSalesProcessRequest{
		Name:               "Laura Beispiel",
		Email:              "laura@example.com",
		Source:             "organic",
		InitialContactDate: &initial,
	}
	b, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/sales/start", bytes.NewReader(b))
	w := httptest.NewRecorder()

	// 1) resolveLeadForSalesStart: email lookup finds unconverted lead id=5
	mock.ExpectQuery("SELECT id FROM leads").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(5))

	// 2) Enrich req from lead (source/stage)
	mock.ExpectQuery("SELECT source, source_stage_id FROM leads WHERE id = ").
		WillReturnRows(sqlmock.NewRows([]string{"source", "source_stage_id"}).
			AddRow(sql.NullString{String: "organic", Valid: true}, sql.NullInt64{Valid: false}))

	// 3) Begin transaction
	mock.ExpectBegin()

	// 4) Insert new client → id=8
	mock.ExpectQuery("INSERT INTO clients").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(8))

	// 5) UPDATE existing lead: set converted_client_id=8 only (converted stays FALSE)
	mock.ExpectExec("UPDATE leads").
		WillReturnResult(sqlmock.NewResult(0, 1))

	// 6) Backfill client status
	mock.ExpectExec("UPDATE clients").
		WillReturnResult(sqlmock.NewResult(0, 0))

	// 7) Insert sales_process → id=15
	mock.ExpectQuery("INSERT INTO sales_process").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(15))

	// 8) Commit
	mock.ExpectCommit()

	// 9) Comments (empty)
	mock.ExpectQuery("FROM comments").
		WillReturnRows(sqlmock.NewRows([]string{"id", "client_id", "entity_type", "entity_id", "author", "body", "metadata", "created_at", "updated_at"}))

	// 10) Client detail
	mock.ExpectQuery("FROM clients").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "email", "phone", "source", "source_stage_id"}).
			AddRow(8, "Laura Beispiel", "laura@example.com",
				sql.NullString{Valid: false},
				sql.NullString{String: "organic", Valid: true},
				sql.NullInt64{Valid: false}))

	h.StartSalesProcess(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp StartSalesProcessResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.SalesProcess.LeadID == nil {
		t.Fatal("expected SalesProcess.LeadID to be non-nil for client created from pre-existing lead")
	}
	if *resp.SalesProcess.LeadID != 5 {
		t.Fatalf("expected lead_id=5 (pre-existing lead), got %d", *resp.SalesProcess.LeadID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestStartSalesProcess_ExistingProcessBackfillsNullStageID(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}

	initial := "2026-04-01"
	sourceStageID := 9
	clientID := 8
	reqBody := StartSalesProcessRequest{
		Name:               "Laura Beispiel",
		Email:              "laura@example.com",
		InitialContactDate: &initial,
		ClientID:           &clientID,
		SourceStageID:      &sourceStageID,
	}
	b, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/sales/start", bytes.NewReader(b))
	w := httptest.NewRecorder()

	// 1) Existing client details lookup.
	mock.ExpectQuery("SELECT name, phone, source").
		WithArgs(clientID).
		WillReturnRows(sqlmock.NewRows([]string{"name", "phone", "source"}).
			AddRow("Laura Beispiel", sql.NullString{Valid: false}, sql.NullString{String: "organic", Valid: true}))

	// 2) No active contract.
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs(clientID).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	// 3) Begin tx.
	mock.ExpectBegin()

	// 4) Backfill client status.
	mock.ExpectExec("UPDATE clients").
		WillReturnResult(sqlmock.NewResult(0, 0))

	// 5) sales_process insert conflicts (no row returned).
	mock.ExpectQuery("INSERT INTO sales_process").
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	// 6) Reuse existing sales_process id=15.
	mock.ExpectQuery("SELECT id FROM sales_process WHERE client_id = ").
		WithArgs(clientID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(15))

	// 7) Backfill stage_id only when currently NULL.
	mock.ExpectExec("UPDATE sales_process SET stage_id = ").
		WithArgs(&sourceStageID, 15).
		WillReturnResult(sqlmock.NewResult(0, 1))

	// 8) Commit.
	mock.ExpectCommit()

	// 9) Comments (empty).
	mock.ExpectQuery("FROM comments").
		WillReturnRows(sqlmock.NewRows([]string{"id", "client_id", "entity_type", "entity_id", "author", "body", "metadata", "created_at", "updated_at"}))

	// 10) Client detail.
	mock.ExpectQuery("FROM clients").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "email", "phone", "source", "source_stage_id"}).
			AddRow(8, "Laura Beispiel", "laura@example.com",
				sql.NullString{Valid: false},
				sql.NullString{String: "organic", Valid: true},
				sql.NullInt64{Int64: 9, Valid: true}))

	h.StartSalesProcess(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp StartSalesProcessResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.SalesProcess.StageID == nil || *resp.SalesProcess.StageID != 9 {
		t.Fatalf("expected stage_id=9, got %#v", resp.SalesProcess.StageID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestStartSalesProcess_ExistingProcessWithoutSourceStageID_DoesNotBackfill(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}

	initial := "2026-04-01"
	clientID := 8
	reqBody := StartSalesProcessRequest{
		Name:               "Laura Beispiel",
		InitialContactDate: &initial,
		ClientID:           &clientID,
	}
	b, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/sales/start", bytes.NewReader(b))
	w := httptest.NewRecorder()

	// 1) Existing client details lookup.
	mock.ExpectQuery("SELECT name, phone, source").
		WithArgs(clientID).
		WillReturnRows(sqlmock.NewRows([]string{"name", "phone", "source"}).
			AddRow("Laura Beispiel", sql.NullString{Valid: false}, sql.NullString{String: "organic", Valid: true}))

	// 2) No active contract.
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs(clientID).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	// 3) Begin tx.
	mock.ExpectBegin()

	// 4) Backfill client status.
	mock.ExpectExec("UPDATE clients").
		WillReturnResult(sqlmock.NewResult(0, 0))

	// 5) sales_process insert conflicts (no row returned).
	mock.ExpectQuery("INSERT INTO sales_process").
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	// 6) Reuse existing sales_process id=15.
	mock.ExpectQuery("SELECT id FROM sales_process WHERE client_id = ").
		WithArgs(clientID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(15))

	// 7) Commit (no stage backfill update expected because SourceStageID is nil).
	mock.ExpectCommit()

	// 8) Comments (empty).
	mock.ExpectQuery("FROM comments").
		WillReturnRows(sqlmock.NewRows([]string{"id", "client_id", "entity_type", "entity_id", "author", "body", "metadata", "created_at", "updated_at"}))

	// 9) Client detail.
	mock.ExpectQuery("FROM clients").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "email", "phone", "source", "source_stage_id"}).
			AddRow(8, "Laura Beispiel", "laura@example.com",
				sql.NullString{Valid: false},
				sql.NullString{String: "organic", Valid: true},
				sql.NullInt64{Valid: false}))

	h.StartSalesProcess(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp StartSalesProcessResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.SalesProcess.StageID != nil {
		t.Fatalf("expected nil stage_id, got %#v", resp.SalesProcess.StageID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestCreateOrUpdateUpsell_StartBeforePrevEnd_Returns422(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}

	result := "verlaengerung"
	revenue := 1200.0
	duration := 6
	freq := "monthly"
	newStart := "2026-03-01" // before previous contract end 2026-06-01

	reqBody := CreateUpsellRequest{
		UpsellResult:           &result,
		UpsellRevenue:          &revenue,
		ContractStartDate:      &newStart,
		ContractDurationMonths: &duration,
		ContractFrequency:      &freq,
	}
	b, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPatch, "/api/sales/1/upsell", bytes.NewReader(b))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	// 1) Resolve client_id
	mock.ExpectQuery("SELECT client_id FROM sales_process WHERE id").
		WithArgs(1).
		WillReturnRows(sqlmock.NewRows([]string{"client_id"}).AddRow(10))

	mock.ExpectBegin()

	// 2) Look up existing open upsell (none)
	mock.ExpectQuery("SELECT id FROM contract_upsells").
		WithArgs(1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	// 3) Find previous contract
	prevContractID := 99
	mock.ExpectQuery("SELECT id FROM contracts").
		WithArgs(10).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(prevContractID))

	// 4) Previous contract end_date > proposed new start
	prevEnd := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery("SELECT end_date FROM contracts WHERE id").
		WithArgs(prevContractID).
		WillReturnRows(sqlmock.NewRows([]string{"end_date"}).AddRow(prevEnd))

	mock.ExpectRollback()

	w := httptest.NewRecorder()
	h.CreateOrUpdateUpsell(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
