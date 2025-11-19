package api_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	_ "github.com/mattn/go-sqlite3"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/api"
)

// --- Schema setup ---
func createContractSchema(t *testing.T, db *sql.DB) {
	_, err := db.Exec(`
	CREATE TABLE clients (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT
	);
	CREATE TABLE contracts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		client_id INTEGER,
		sales_process_id INTEGER,
		start_date TEXT,
		end_date_computed TEXT,
		duration_months INTEGER,
		revenue_total REAL,
		payment_frequency TEXT
	);
	`)
	if err != nil {
		t.Fatalf("schema error: %v", err)
	}
	_, _ = db.Exec(`INSERT INTO clients (name) VALUES ('Acme Inc');`)
	_, _ = db.Exec(`
	INSERT INTO contracts (client_id, sales_process_id, start_date, duration_months, revenue_total, payment_frequency)
	VALUES (1, 1, '2024-01-01', 12, 12000, 'monthly');
	`)
}

// --- CreateContract ---
func TestCreateContract_Success(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	createContractSchema(t, db)

	h := &api.Handler{DB: db}
	c := api.Contract{
		ClientID:       1,
		SalesProcessID: 2,
		StartDate:      "2025-04-01",
		DurationMonths: 6,
		RevenueTotal:   6000,
		PaymentFreq:    "monthly",
	}
	body, _ := json.Marshal(c)
	req := httptest.NewRequest(http.MethodPost, "/api/contracts", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.CreateContract(w, req)
	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}
}

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
	// Closed DB simulates insert failure
	db, _ := sql.Open("sqlite3", ":memory:")
	db.Close()
	h := &api.Handler{DB: db}

	valid := api.Contract{ClientID: 1, StartDate: "2024-01-01", DurationMonths: 3, RevenueTotal: 1000, PaymentFreq: "monthly"}
	b, _ := json.Marshal(valid)
	req := httptest.NewRequest(http.MethodPost, "/api/contracts", bytes.NewReader(b))
	w := httptest.NewRecorder()

	h.CreateContract(w, req)

	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Result().StatusCode)
	}
}

// --- UpdateContract ---
func TestUpdateContract_Success(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	createContractSchema(t, db)

	h := &api.Handler{DB: db}
	end := "2025-11-01"
	update := api.Contract{EndDate: &end, RevenueTotal: 15000}
	body, _ := json.Marshal(update)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")

	req := httptest.NewRequest(http.MethodPatch, "/api/contracts/1", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	h.UpdateContract(w, req)
	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
}

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
	createContractSchema(t, db)

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

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")

	end := "2026-01-01"
	update := api.Contract{EndDate: &end, RevenueTotal: 2000}
	body, _ := json.Marshal(update)

	req := httptest.NewRequest(http.MethodPatch, "/api/contracts/1", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	h.UpdateContract(w, req)

	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Result().StatusCode)
	}
}
