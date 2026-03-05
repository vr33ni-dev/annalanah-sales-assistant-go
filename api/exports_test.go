package api_test

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/api"
)

func createExportTestSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	stmts := []string{
		`CREATE TABLE clients (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT,
			email TEXT,
			phone TEXT,
			source TEXT,
			source_stage_id INTEGER,
			status TEXT,
			completed_at TEXT,
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
			payment_frequency TEXT,
			created_at TEXT,
			updated_at TEXT
		);`,
		`CREATE TABLE cashflow_entries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			contract_id INTEGER,
			due_date TEXT,
			amount REAL,
			status TEXT,
			updated_at TEXT
		);`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("schema create failed: %v", err)
		}
	}
}

func TestExportRawClientsCSV(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	createExportTestSchema(t, db)

	_, _ = db.Exec(`INSERT INTO clients (name,email,phone,source,status,created_at) VALUES ('Alice','alice@example.com','123','organic','active','2026-01-01')`)

	h := &api.Handler{DB: db}
	req := httptest.NewRequest(http.MethodGet, "/api/exports/raw/clients.csv", nil)
	w := httptest.NewRecorder()

	h.ExportRawClientsCSV(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/csv") {
		t.Fatalf("expected csv content-type, got %q", ct)
	}
	body := w.Body.String()
	if !strings.Contains(body, "id,name,email,phone,source,source_stage_id,status,completed_at,created_at") {
		t.Fatalf("missing expected csv header, body=%s", body)
	}
	if !strings.Contains(body, "alice@example.com") {
		t.Fatalf("missing inserted client row, body=%s", body)
	}
}

func TestExportAggregatedCashflowCSV(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	createExportTestSchema(t, db)

	_, _ = db.Exec(`INSERT INTO clients (id,name,email,phone,source,status) VALUES (1,'Alice','alice@example.com','123','organic','active')`)
	_, _ = db.Exec(`INSERT INTO contracts (id,client_id,sales_process_id,start_date,end_date,duration_months,revenue_total,payment_frequency) VALUES (10,1,100,'2026-01-15','2026-03-20',3,300,'monthly')`)
	_, _ = db.Exec(`INSERT INTO cashflow_entries (contract_id,due_date,amount,status) VALUES (10,'2026-01-20',100,'pending')`)
	_, _ = db.Exec(`INSERT INTO cashflow_entries (contract_id,due_date,amount,status) VALUES (10,'2026-02-20',100,'pending')`)

	h := &api.Handler{DB: db}
	req := httptest.NewRequest(http.MethodGet, "/api/exports/aggregated/cashflow.csv?from=2026-01&to=2026-03", nil)
	w := httptest.NewRecorder()

	h.ExportAggregatedCashflowCSV(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "m_2026_01") || !strings.Contains(body, "m_2026_02") || !strings.Contains(body, "m_2026_03") {
		t.Fatalf("missing month columns in header, body=%s", body)
	}
	if !strings.Contains(body, "100.00,100.00,0.00") {
		t.Fatalf("expected aggregated monthly amounts, body=%s", body)
	}
}
