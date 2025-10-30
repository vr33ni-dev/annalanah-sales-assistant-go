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

func createSalesSchema(db *sql.DB, t *testing.T) {
	stmts := []string{
		`CREATE TABLE clients (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT,
			email TEXT,
			phone TEXT,
			source TEXT,
			source_stage_id INTEGER,
			status TEXT,
			completed_at TEXT
		);`,
		`CREATE TABLE sales_process (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			client_id INTEGER,
			stage TEXT,
			follow_up_date TEXT,
			follow_up_result BOOLEAN,
			closed BOOLEAN,
			revenue REAL,
			stage_id INTEGER,
			created_at TEXT
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
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("schema create failed: %v", err)
		}
	}

	// ✅ required seed rows for UpdateSalesProcess to work
	_, _ = db.Exec(`INSERT INTO clients (id, name, email, phone, source, status) 
	                VALUES (1, 'Alice', 'a@example.com', '123', 'web', 'follow_up_scheduled')`)
	_, _ = db.Exec(`INSERT INTO sales_process (id, client_id, stage, follow_up_result, closed, revenue, stage_id, created_at)
	                VALUES (1, 1, 'follow_up', NULL, NULL, NULL, NULL, '2025-01-01')`)
}

// --- Tests ---

func TestListSalesProcesses(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	createSalesSchema(db, t)

	h := &api.Handler{DB: db}

	req := httptest.NewRequest(http.MethodGet, "/api/sales", nil)
	w := httptest.NewRecorder()

	h.ListSalesProcesses(w, req)
	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	var got []api.SalesProcessResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(got) != 1 || got[0].ClientName != "Alice" {
		t.Fatalf("expected one record with client=Alice, got %+v", got)
	}
}

func TestCreateSalesProcess_NewOK(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	createSalesSchema(db, t)
	// insert a second client with no sales_process
	_, _ = db.Exec(`INSERT INTO clients (name, email, phone, source, status) VALUES ('Bob', 'b@example.com', '555', 'referral', 'follow_up_scheduled')`)

	h := &api.Handler{DB: db}

	body := api.SalesProcess{
		ClientID: 2, // ✅ new client without a process
		Stage:    "follow_up",
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/sales", bytes.NewReader(b))
	w := httptest.NewRecorder()

	h.CreateSalesProcess(w, req)
	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	var created api.SalesProcess
	_ = json.NewDecoder(resp.Body).Decode(&created)
	if created.ID == 0 {
		t.Fatal("expected new ID assigned")
	}
}

func TestCreateSalesProcess_DuplicateClient(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	createSalesSchema(db, t)
	h := &api.Handler{DB: db}

	// already has one process for client 1
	body := api.SalesProcess{ClientID: 1, Stage: "lost"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/sales", bytes.NewReader(b))
	w := httptest.NewRecorder()

	h.CreateSalesProcess(w, req)
	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for duplicate client, got %d", resp.StatusCode)
	}
}

func TestUpdateSalesProcess_ClosedValidationFails(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	createSalesSchema(db, t)
	h := &api.Handler{DB: db}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")

	closed := true
	body := map[string]any{"closed": closed}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPatch, "/api/sales/1", bytes.NewReader(b))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	h.UpdateSalesProcess(w, req)
	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing contract details, got %d", resp.StatusCode)
	}
}

func TestStartSalesProcess(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	createSalesSchema(db, t)
	h := &api.Handler{DB: db}

	reqBody := api.StartSalesProcessRequest{
		Name:         "Bob",
		Email:        "b@example.com",
		Phone:        "999",
		Source:       "organic",
		FollowUpDate: strPtr("2025-11-01"),
	}
	b, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/sales/start", bytes.NewReader(b))
	w := httptest.NewRecorder()

	h.StartSalesProcess(w, req)
	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d", resp.StatusCode)
	}

	var out api.StartSalesProcessResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}
	if out.Client.Name != "Bob" {
		t.Fatalf("expected client=Bob, got %+v", out.Client)
	}
	if out.SalesProcess.Stage != "follow_up" {
		t.Fatalf("expected stage=follow_up, got %s", out.SalesProcess.Stage)
	}
}

func strPtr(s string) *string { return &s }
