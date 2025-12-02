package api_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	_ "github.com/mattn/go-sqlite3"

	"github.com/vr33ni-dev/annalanah-sales-assistant-go/api"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/api/testhelpers"
)

//
// ----------------------------------------------------------
// CREATE CONTRACT (success path – real PostgreSQL)
// ----------------------------------------------------------
//

func TestCreateContract_Success(t *testing.T) {
	s := testhelpers.NewAPITestSuite(t)
	defer s.TearDown()
	s.Cleanup(t) // fresh DB

	client := s.CreateClient()
	proc := s.CreateSalesProcessForClient(client.ID)

	body, _ := json.Marshal(api.Contract{
		ClientID:       client.ID,
		SalesProcessID: proc.ID,
		StartDate:      "2025-04-01",
		DurationMonths: 6,
		RevenueTotal:   6000,
		PaymentFreq:    "monthly",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/contracts", bytes.NewReader(body))
	w := httptest.NewRecorder()

	s.Handler.CreateContract(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

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
// UPDATE CONTRACT (success path – real PostgreSQL)
// ----------------------------------------------------------
//

func TestUpdateContract_Success(t *testing.T) {
	s := testhelpers.NewAPITestSuite(t)
	defer s.TearDown()
	s.Cleanup(t)

	client := s.CreateClient()
	proc := s.CreateSalesProcessForClient(client.ID)
	contract := s.CreateContract(client.ID, proc.ID)

	// Prepare update payload
	end := "2025-11-01"
	update := api.Contract{
		EndDate:      &end,
		RevenueTotal: 15000,
	}
	body, _ := json.Marshal(update)

	// Inject route param
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", fmt.Sprint(contract.ID))

	req := httptest.NewRequest(http.MethodPatch, "/api/contracts/1", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	s.Handler.UpdateContract(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
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
	db, _ := sql.Open("sqlite3", ":memory:")
	db.Close() // simulate broken DB

	h := &api.Handler{DB: db}

	end := "2026-01-01"
	update := api.Contract{
		EndDate:      &end,
		RevenueTotal: 2000,
	}

	body, _ := json.Marshal(update)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")

	req := httptest.NewRequest(http.MethodPatch, "/api/contracts/1", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	h.UpdateContract(w, req)

	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Result().StatusCode)
	}
}
