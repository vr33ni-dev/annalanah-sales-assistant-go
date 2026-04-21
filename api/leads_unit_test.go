package api_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-chi/chi/v5"
	"github.com/lib/pq"

	"github.com/vr33ni-dev/annalanah-sales-assistant-go/api"
)

func TestListLeads_Success(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := &api.Handler{DB: db}

	rows := sqlmock.NewRows([]string{
		"id", "name", "email", "phone", "source",
		"source_stage_id", "source_stage_name", "converted", "created_at",
	}).AddRow(
		1, "Alice", "a@test.com", "123", "organic", nil, "Follow Up", false, sql.NullTime{},
	)

	mock.ExpectQuery(`FROM leads`).
		WillReturnRows(rows)

	req := httptest.NewRequest(http.MethodGet, "/api/leads", nil)
	w := httptest.NewRecorder()

	h.ListLeads(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var out []api.LeadResponse
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("invalid json: %v", err)
	}

	if len(out) != 1 || out[0].Name != "Alice" {
		t.Fatalf("unexpected response %+v", out)
	}
}

func TestListLeads_DBError(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := &api.Handler{DB: db}

	mock.ExpectQuery(`FROM leads`).
		WillReturnError(sql.ErrConnDone)

	req := httptest.NewRequest(http.MethodGet, "/api/leads", nil)
	w := httptest.NewRecorder()

	h.ListLeads(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestCreateLead_BadJSON(t *testing.T) {
	h := &api.Handler{DB: nil}

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/leads",
		bytes.NewReader([]byte("{bad json")),
	)
	w := httptest.NewRecorder()

	h.CreateLead(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCreateLead_Success(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := &api.Handler{DB: db}

	mock.ExpectQuery(`INSERT INTO leads`).
		WithArgs("Bob", "b@test.com", "999", "paid", sql.NullInt64{}).
		WillReturnRows(
			sqlmock.NewRows([]string{"id", "created_at"}).
				AddRow(10, sql.NullTime{}),
		)

	body := map[string]interface{}{
		"name":   "Bob",
		"email":  "b@test.com",
		"phone":  "999",
		"source": "paid",
	}

	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/leads", bytes.NewReader(b))
	w := httptest.NewRecorder()

	h.CreateLead(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}
}

func TestCreateLead_DuplicateEmail(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := &api.Handler{DB: db}

	mock.ExpectQuery(`INSERT INTO leads`).
		WillReturnError(&pq.Error{Code: "23505"})

	mock.ExpectQuery(`FROM leads l`).
		WithArgs("dup@test.com").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "email", "phone", "source",
			"source_stage_id", "source_stage_name", "converted", "created_at",
		}).AddRow(
			5, "Dup", "dup@test.com", "", "organic", nil, "", false, sql.NullTime{},
		))

	body := map[string]interface{}{
		"name":   "Dup",
		"email":  "dup@test.com",
		"source": "organic",
	}

	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/leads", bytes.NewReader(b))
	w := httptest.NewRecorder()

	h.CreateLead(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestUpdateLead_InvalidID(t *testing.T) {
	h := &api.Handler{DB: nil}

	req := httptest.NewRequest(http.MethodPatch, "/api/leads/abc", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "abc")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	h.UpdateLead(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpdateLead_NoFields(t *testing.T) {
	h := &api.Handler{DB: nil}

	req := httptest.NewRequest(http.MethodPatch, "/api/leads/1", bytes.NewReader([]byte(`{}`)))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	h.UpdateLead(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestDeleteLead_Success(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := &api.Handler{DB: db}

	mock.ExpectExec(`DELETE FROM leads`).
		WithArgs(3).
		WillReturnResult(sqlmock.NewResult(0, 1))

	req := httptest.NewRequest(http.MethodDelete, "/api/leads/3", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "3")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	h.DeleteLead(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

func TestUpdateLead_Success(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := &api.Handler{DB: db}

	mock.ExpectQuery(`(?s)UPDATE\s+leads`).
		WithArgs(
			sqlmock.AnyArg(), // name
			sqlmock.AnyArg(), // email
			sqlmock.AnyArg(), // phone
			sqlmock.AnyArg(), // source
			sqlmock.AnyArg(), // source_stage_id (allow any sql.NullInt64)
			1,                // id
		).
		WillReturnRows(
			sqlmock.NewRows([]string{
				"id", "name", "email", "phone", "source",
				"source_stage_id", "source_stage_name", "created_at",
			}).AddRow(
				1, "Alice", "a@test.com", "123", "organic", nil, "", sql.NullTime{},
			),
		)

	body := map[string]string{"name": "Alice"}
	b, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPatch, "/api/leads/1", bytes.NewReader(b))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	h.UpdateLead(w, req)

	if w.Code != http.StatusOK {
		t.Logf("response body: %s", w.Body.String())
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Logf("unmet sqlmock expectations: %v", err)
		}
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestUpdateLead_NotFound(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := &api.Handler{DB: db}

	mock.ExpectQuery(`UPDATE leads SET`).
		WillReturnError(sql.ErrNoRows)

	req := httptest.NewRequest(http.MethodPatch, "/api/leads/99", bytes.NewReader([]byte(`{"name":"X"}`)))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "99")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	h.UpdateLead(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestDeleteLead_NotFound(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := &api.Handler{DB: db}

	mock.ExpectExec(`DELETE FROM leads`).
		WithArgs(9).
		WillReturnResult(sqlmock.NewResult(0, 0))

	req := httptest.NewRequest(http.MethodDelete, "/api/leads/9", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "9")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	h.DeleteLead(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestConvertLead_InvalidID(t *testing.T) {
	h := &api.Handler{DB: nil}

	req := httptest.NewRequest(http.MethodPost, "/api/leads/abc/convert", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "abc")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	h.ConvertLead(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestConvertLead_NotFound(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := &api.Handler{DB: db}

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT name, email, phone, source`).
		WithArgs(1).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	req := httptest.NewRequest(http.MethodPost, "/api/leads/1/convert", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	h.ConvertLead(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestConvertLead_Success(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := &api.Handler{DB: db}

	// Begin transaction and select lead
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT name, email, phone, source`).
		WithArgs(1).
		WillReturnRows(sqlmock.NewRows([]string{"name", "email", "phone", "source", "source_stage_id"}).
			AddRow("LeadName", sql.NullString{String: "lead@test.com", Valid: true}, sql.NullString{String: "555", Valid: true}, "organic", nil))

	// createClientAndSalesProcessTx: clients lookup -> not found
	mock.ExpectQuery(`SELECT id FROM clients WHERE LOWER\(email\) = LOWER\(\$1\)`).
		WithArgs("lead@test.com").
		WillReturnError(sql.ErrNoRows)

	// insert client
	mock.ExpectQuery(`INSERT INTO clients`).
		WithArgs("LeadName", sqlmock.AnyArg(), sqlmock.AnyArg(), "organic", sqlmock.AnyArg(), "follow_up_scheduled").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(11))

	// insert sales_process
	mock.ExpectQuery(`INSERT INTO sales_process`).
		WithArgs(11, sqlmock.AnyArg(), sqlmock.AnyArg(), "follow_up", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(21))

	// update lead converted
	mock.ExpectExec(`UPDATE leads`).
		WithArgs(11, 1).
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectCommit()

	req := httptest.NewRequest(http.MethodPost, "/api/leads/1/convert", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	h.ConvertLead(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body=%s", w.Code, w.Body.String())
	}

	var out map[string]int
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("invalid json: %v", err)
	}

	if out["client_id"] != 11 || out["sales_process_id"] != 21 {
		t.Fatalf("unexpected response %+v", out)
	}
}

func TestConvertLead_BeginTxError(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := &api.Handler{DB: db}

	mock.ExpectBegin().WillReturnError(sql.ErrConnDone)

	req := httptest.NewRequest(http.MethodPost, "/api/leads/1/convert", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	h.ConvertLead(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestConvertLead_LeadQueryError(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := &api.Handler{DB: db}

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT name, email, phone, source`).
		WithArgs(2).
		WillReturnError(sql.ErrConnDone)
	mock.ExpectRollback()

	req := httptest.NewRequest(http.MethodPost, "/api/leads/2/convert", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "2")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	h.ConvertLead(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestConvertLead_CreateClientSalesError(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := &api.Handler{DB: db}

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT name, email, phone, source`).
		WithArgs(3).
		WillReturnRows(sqlmock.NewRows([]string{"name", "email", "phone", "source", "source_stage_id"}).
			AddRow("Bob", sql.NullString{}, sql.NullString{}, "organic", nil))
	// No email → INSERT INTO clients directly
	mock.ExpectQuery(`INSERT INTO clients`).
		WillReturnError(sql.ErrConnDone)
	mock.ExpectRollback()

	req := httptest.NewRequest(http.MethodPost, "/api/leads/3/convert", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "3")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	h.ConvertLead(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestConvertLead_UniqueClientSalesConstraint(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := &api.Handler{DB: db}

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT name, email, phone, source`).
		WithArgs(4).
		WillReturnRows(sqlmock.NewRows([]string{"name", "email", "phone", "source", "source_stage_id"}).
			AddRow("Carol", sql.NullString{}, sql.NullString{}, "referral", nil))
	// No email → INSERT INTO clients
	mock.ExpectQuery(`INSERT INTO clients`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(55))
	// INSERT INTO sales_process → conflicts on unique_client_sales
	mock.ExpectQuery(`INSERT INTO sales_process`).
		WillReturnError(&pq.Error{Code: "23505", Constraint: "unique_client_sales"})
	mock.ExpectRollback()
	// After rollback, reload existing sales_process via h.DB
	mock.ExpectQuery(`SELECT id FROM sales_process WHERE client_id`).
		WithArgs(55).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(99))

	req := httptest.NewRequest(http.MethodPost, "/api/leads/4/convert", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "4")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	h.ConvertLead(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var out map[string]int
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if out["client_id"] != 55 || out["sales_process_id"] != 99 {
		t.Fatalf("unexpected response: %+v", out)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// ─── ListLeads additional paths ───────────────────────────────────────────────

func TestListLeads_ScanError(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := &api.Handler{DB: db}

	// Wrong column count triggers scan error inside the loop
	rows := sqlmock.NewRows([]string{"id"}).AddRow(1)
	mock.ExpectQuery("SELECT").WillReturnRows(rows)

	req := httptest.NewRequest(http.MethodGet, "/api/leads", nil)
	w := httptest.NewRecorder()
	h.ListLeads(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

// ─── CreateLead additional paths ─────────────────────────────────────────────

func TestCreateLead_StageByName(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := &api.Handler{DB: db}

	stageName := "Follow Up"
	payload := map[string]interface{}{
		"name":              "Alice",
		"email":             "alice@test.com",
		"source":            "organic",
		"source_stage_name": stageName,
	}
	b, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/leads", bytes.NewReader(b))
	w := httptest.NewRecorder()

	// Stage lookup by name
	mock.ExpectQuery("SELECT id FROM stages WHERE name").
		WithArgs(stageName).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(7))
	// INSERT lead with the resolved stage id
	mock.ExpectQuery("INSERT INTO leads").
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(11, sql.NullTime{}))

	h.CreateLead(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var out api.LeadResponse
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.SourceStageID == nil || *out.SourceStageID != 7 {
		t.Fatalf("expected SourceStageID=7, got %v", out.SourceStageID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestCreateLead_DuplicateEmail_NilEmail(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := &api.Handler{DB: db}

	payload := map[string]interface{}{"name": "Bob", "source": "referral"}
	b, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/leads", bytes.NewReader(b))
	w := httptest.NewRecorder()

	// INSERT fails with 23505 even though email is nil (forced via mock)
	mock.ExpectQuery("INSERT INTO leads").
		WillReturnError(&pq.Error{Code: "23505", Constraint: "unique_lead_email"})

	h.CreateLead(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateLead_DuplicateEmail_ScanError(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := &api.Handler{DB: db}

	email := "dup@test.com"
	payload := map[string]interface{}{"name": "Charlie", "email": email, "source": "organic"}
	b, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/leads", bytes.NewReader(b))
	w := httptest.NewRecorder()

	// INSERT fails with 23505
	mock.ExpectQuery("INSERT INTO leads").
		WillReturnError(&pq.Error{Code: "23505", Constraint: "unique_lead_email"})
	// Fetch existing lead also fails
	mock.ExpectQuery("SELECT").
		WillReturnError(sql.ErrConnDone)

	h.CreateLead(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateLead_NonPqError(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := &api.Handler{DB: db}

	payload := map[string]interface{}{"name": "Dave", "email": "dave@test.com", "source": "organic"}
	b, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/leads", bytes.NewReader(b))
	w := httptest.NewRecorder()

	// INSERT fails with non-pq error → 500
	mock.ExpectQuery("INSERT INTO leads").
		WillReturnError(sql.ErrConnDone)

	h.CreateLead(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

// ─── UpdateLead additional paths ─────────────────────────────────────────────

func TestUpdateLead_ScanError(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := &api.Handler{DB: db}

	payload := map[string]interface{}{"name": "Updated"}
	b, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPatch, "/api/leads/1", bytes.NewReader(b))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	// UPDATE returns wrong columns → scan error (not ErrNoRows) → 500
	mock.ExpectQuery("UPDATE leads SET").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))

	h.UpdateLead(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateLead_StageByName(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := &api.Handler{DB: db}

	stageName := "Follow Up"
	payload := map[string]interface{}{"source_stage_name": stageName}
	b, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPatch, "/api/leads/1", bytes.NewReader(b))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	// Stage lookup by name
	mock.ExpectQuery("SELECT id FROM stages WHERE name").
		WithArgs(stageName).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(3))
	// UPDATE RETURNING
	mock.ExpectQuery("UPDATE leads SET").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "email", "phone", "source", "source_stage_id", "source_stage_name", "created_at",
		}).AddRow(1, "Alice", sql.NullString{}, sql.NullString{}, "organic",
			sql.NullInt64{Int64: 3, Valid: true}, "Follow Up", sql.NullTime{}))

	h.UpdateLead(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// ─── DeleteLead additional paths ─────────────────────────────────────────────

func TestDeleteLead_ExecError(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := &api.Handler{DB: db}

	mock.ExpectExec("DELETE FROM leads WHERE id").
		WithArgs(1).
		WillReturnError(sql.ErrConnDone)

	req := httptest.NewRequest(http.MethodDelete, "/api/leads/1", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	h.DeleteLead(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

// ─── ConvertLead additional paths ────────────────────────────────────────────

func TestConvertLead_UpdateLeadExecError(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := &api.Handler{DB: db}

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT name, email, phone, source`).
		WithArgs(1).
		WillReturnRows(sqlmock.NewRows([]string{"name", "email", "phone", "source", "source_stage_id"}).
			AddRow("LeadName", sql.NullString{String: "lead@test.com", Valid: true}, sql.NullString{String: "555", Valid: true}, "organic", nil))
	mock.ExpectQuery(`SELECT id FROM clients WHERE LOWER\(email\) = LOWER\(\$1\)`).
		WithArgs("lead@test.com").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`INSERT INTO clients`).
		WithArgs("LeadName", sqlmock.AnyArg(), sqlmock.AnyArg(), "organic", sqlmock.AnyArg(), "follow_up_scheduled").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(11))
	mock.ExpectQuery(`INSERT INTO sales_process`).
		WithArgs(11, sqlmock.AnyArg(), sqlmock.AnyArg(), "follow_up", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(21))
	// UPDATE leads fails
	mock.ExpectExec(`UPDATE leads`).
		WithArgs(11, 1).
		WillReturnError(sql.ErrConnDone)
	mock.ExpectRollback()

	req := httptest.NewRequest(http.MethodPost, "/api/leads/1/convert", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	h.ConvertLead(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestConvertLead_CommitError(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := &api.Handler{DB: db}

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT name, email, phone, source`).
		WithArgs(1).
		WillReturnRows(sqlmock.NewRows([]string{"name", "email", "phone", "source", "source_stage_id"}).
			AddRow("LeadName", sql.NullString{String: "lead@test.com", Valid: true}, sql.NullString{String: "555", Valid: true}, "organic", nil))
	mock.ExpectQuery(`SELECT id FROM clients WHERE LOWER\(email\) = LOWER\(\$1\)`).
		WithArgs("lead@test.com").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`INSERT INTO clients`).
		WithArgs("LeadName", sqlmock.AnyArg(), sqlmock.AnyArg(), "organic", sqlmock.AnyArg(), "follow_up_scheduled").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(11))
	mock.ExpectQuery(`INSERT INTO sales_process`).
		WithArgs(11, sqlmock.AnyArg(), sqlmock.AnyArg(), "follow_up", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(21))
	mock.ExpectExec(`UPDATE leads`).
		WithArgs(11, 1).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit().WillReturnError(sql.ErrConnDone)

	req := httptest.NewRequest(http.MethodPost, "/api/leads/1/convert", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	h.ConvertLead(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestConvertLead_UniqueClientEmail(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := &api.Handler{DB: db}

	email := "existing@test.com"

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT name, email, phone, source`).
		WithArgs(5).
		WillReturnRows(sqlmock.NewRows([]string{"name", "email", "phone", "source", "source_stage_id"}).
			AddRow("Eve", sql.NullString{String: email, Valid: true}, sql.NullString{}, "organic", nil))
	// createClientAndSalesProcessTx: lookup by email → not found
	mock.ExpectQuery(`SELECT id FROM clients WHERE LOWER\(email\) = LOWER\(\$1\)`).
		WithArgs(email).
		WillReturnError(sql.ErrNoRows)
	// INSERT clients → unique_client_email constraint
	mock.ExpectQuery(`INSERT INTO clients`).
		WillReturnError(&pq.Error{Code: "23505", Constraint: "unique_client_email"})
	mock.ExpectRollback()
	// emailPtr is valid → find existing client by email (via h.DB directly)
	mock.ExpectQuery(`SELECT id FROM clients WHERE email`).
		WithArgs(email).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(77))
	// INSERT sales_process for existing client
	mock.ExpectQuery(`INSERT INTO sales_process`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(88))

	req := httptest.NewRequest(http.MethodPost, "/api/leads/5/convert", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "5")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	h.ConvertLead(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var out map[string]int
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["client_id"] != 77 || out["sales_process_id"] != 88 {
		t.Fatalf("unexpected response: %+v", out)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
