package api_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-chi/chi/v5"
	_ "github.com/mattn/go-sqlite3"

	"github.com/vr33ni-dev/annalanah-sales-assistant-go/api"
)

/*
1️⃣ Success-path tests → use Embedded Postgres + testhelpers
These tests require:
real schema
real migrations
real foreign key constraints
real SQL behavior (Postgres syntax, RETURNING, computed columns, etc.)

2️⃣ Error-path tests → use SQLite
These tests do NOT need schema, migrations, or Postgres.2
*/

//
// ----------------------------------------------------------
// CREATE CONTRACT (error paths – SQLite only)
// ----------------------------------------------------------
//

func TestCreateContract_BadJSON(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()

	h := &api.Handler{DB: db}

	req := httptest.NewRequest(http.MethodPost, "/api/contracts", strings.NewReader("{bad json"))
	w := httptest.NewRecorder()

	h.CreateContract(w, req)

	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Result().StatusCode)
	}
}

func TestCreateContract_DBError(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	db.Close() // simulate broken DB

	h := &api.Handler{DB: db}

	valid := api.Contract{
		ClientID:       1,
		StartDate:      "2024-01-01",
		DurationMonths: 3,
		RevenueTotal:   1000,
		PaymentFreq:    "monthly",
	}

	b, _ := json.Marshal(valid)

	req := httptest.NewRequest(http.MethodPost, "/api/contracts", bytes.NewReader(b))
	w := httptest.NewRecorder()

	h.CreateContract(w, req)

	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Result().StatusCode)
	}
}

//
// ----------------------------------------------------------
// UPDATE CONTRACT (error paths – SQLite only)
// ----------------------------------------------------------
//

func TestUpdateContract_InvalidID(t *testing.T) {
	h := &api.Handler{DB: nil}

	req := httptest.NewRequest(http.MethodPatch, "/api/contracts/abc", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "abc")

	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	h.UpdateContract(w, req)

	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Result().StatusCode)
	}
}

func TestUpdateContract_BadJSON(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()

	h := &api.Handler{DB: db}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")

	req := httptest.NewRequest(http.MethodPatch, "/api/contracts/1", strings.NewReader("{bad json"))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	h.UpdateContract(w, req)

	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Result().StatusCode)
	}
}

func TestUpdateContract_DBError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectExec(`UPDATE\s+contracts`).
		WithArgs(
			sqlmock.AnyArg(), // start_date → time.Time
			12,
			2000.0,
			"monthly",
			1,
		).
		WillReturnError(errors.New("db failure"))

	h := &api.Handler{DB: db}

	body, _ := json.Marshal(map[string]any{
		"start_date":        "2025-05-01",
		"duration_months":   12,
		"revenue_total":     2000,
		"payment_frequency": "monthly",
	})

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")

	req := httptest.NewRequest(http.MethodPatch, "/api/contracts/1", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	h.UpdateContract(w, req)

	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Result().StatusCode)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func TestListContracts_DBError(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	mock.ExpectQuery(`WITH paid AS`).
		WillReturnError(errors.New("db down"))

	h := &api.Handler{DB: db}

	req := httptest.NewRequest(http.MethodGet, "/api/contracts", nil)
	w := httptest.NewRecorder()

	h.ListContracts(w, req)

	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Result().StatusCode)
	}
}
