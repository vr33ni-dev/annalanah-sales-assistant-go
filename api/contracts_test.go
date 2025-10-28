package api_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	_ "github.com/mattn/go-sqlite3"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/api"
)

// helper to create schema and seed data for all tests
func createContractSchema(db *sql.DB, t *testing.T) {
	stmts := []string{
		`CREATE TABLE clients (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT
		);`,
		`CREATE TABLE contracts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			client_id INTEGER,
			sales_process_id INTEGER,
			start_date TEXT,
			end_date TEXT,
			duration_months INTEGER,
			revenue_total REAL,
			payment_frequency TEXT
		);`,
		`CREATE TABLE cashflow_entries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			contract_id INTEGER,
			due_date TEXT,
			amount REAL,
			status TEXT
		);`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("creating schema failed: %v", err)
		}
	}

	// Seed one client and one contract
	_, err := db.Exec(`INSERT INTO clients (name) VALUES ('Acme Inc')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO contracts (client_id, sales_process_id, start_date, end_date, duration_months, revenue_total, payment_frequency)
					  VALUES (1, 1, '2025-01-01', '2025-12-31', 12, 12000, 'monthly')`)
	if err != nil {
		t.Fatal(err)
	}
	// Add a couple of paid cashflow entries
	_, _ = db.Exec(`INSERT INTO cashflow_entries (contract_id, due_date, amount, status)
					VALUES (1, '2025-02-01', 1000, 'paid'),
					       (1, '2025-03-01', 1000, 'paid')`)
}

func TestCreateContract(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	createContractSchema(db, t)

	h := &api.Handler{DB: db}

	body := api.Contract{
		ClientID:       1,
		SalesProcessID: 2,
		StartDate:      "2025-04-01",
		EndDate:        nil,
		DurationMonths: 6,
		RevenueTotal:   6000,
		PaymentFreq:    "monthly",
	}

	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/contracts", bytes.NewReader(b))
	w := httptest.NewRecorder()

	h.CreateContract(w, req)
	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	var got api.Contract
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if got.ID == 0 {
		t.Fatal("expected contract ID to be returned")
	}
	if got.RevenueTotal != 6000 {
		t.Fatalf("expected RevenueTotal=6000, got %v", got.RevenueTotal)
	}
}

func TestUpdateContract(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	createContractSchema(db, t)

	h := &api.Handler{DB: db}

	// prepare PATCH body
	end := "2025-11-01"
	update := api.Contract{EndDate: &end, RevenueTotal: 15000}
	body, _ := json.Marshal(update)

	// inject chi route param correctly ✅
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")

	req := httptest.NewRequest(http.MethodPatch, "/api/contracts/1", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	h.UpdateContract(w, req)
	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 No Content, got %d", resp.StatusCode)
	}

	// Verify DB updated
	var gotEnd string
	var gotTotal float64
	err := db.QueryRow(`SELECT end_date, revenue_total FROM contracts WHERE id=1`).Scan(&gotEnd, &gotTotal)
	if err != nil {
		t.Fatal(err)
	}
	if gotEnd != end {
		t.Fatalf("expected end_date %s, got %s", end, gotEnd)
	}
	if gotTotal != 15000 {
		t.Fatalf("expected revenue_total 15000, got %v", gotTotal)
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
	resp := w.Result()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid id, got %d", resp.StatusCode)
	}
}
