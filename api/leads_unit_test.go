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
