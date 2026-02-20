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
			client_id INTEGER NOT NULL UNIQUE,
			stage TEXT,
			initial_contact_date TEXT,
			follow_up_date TEXT,
			follow_up_result BOOLEAN,
			closed BOOLEAN,
			revenue REAL,
			stage_id INTEGER,
			lead_id INTEGER,
			created_at TEXT
		);`,
	}

	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("schema create failed: %v\nSQL: %s", err, s)
		}
	}

	_, _ = db.Exec(`
		INSERT INTO clients (id, name, email, phone, source, status)
		VALUES (1, 'Alice', 'a@example.com', '123', 'organic', 'follow_up_scheduled')
	`)
	_, _ = db.Exec(`
		INSERT INTO sales_process
			(id, client_id, stage, created_at)
		VALUES
			(1, 1, 'follow_up', '2025-01-01')
	`)
}

func TestListSalesProcesses(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	createSalesSchema(db, t)

	h := &api.Handler{DB: db}

	req := httptest.NewRequest(http.MethodGet, "/api/sales", nil)
	w := httptest.NewRecorder()

	h.ListSalesProcesses(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestUpdateSalesProcess_ClosedValidationFails(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	createSalesSchema(db, t)

	h := &api.Handler{DB: db}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")

	body := map[string]any{"closed": true}
	b, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPatch, "/api/sales/1", bytes.NewReader(b))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	h.UpdateSalesProcess(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
