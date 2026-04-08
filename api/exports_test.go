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
		`CREATE TABLE stages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT
		);`,
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
			source TEXT NOT NULL DEFAULT 'manual',
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

	_, _ = db.Exec(`INSERT INTO stages (id,name) VALUES (3,'Instagram Ads')`)
	_, _ = db.Exec(`INSERT INTO clients (name,email,phone,source,source_stage_id,status,created_at) VALUES ('Alice','alice@example.com','123','organic',3,'active','2026-01-01')`)

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
	if !strings.Contains(body, "id,name,email,phone,source,source_stage_name,source_stage_id,status,completed_at,created_at") {
		t.Fatalf("missing expected csv header, body=%s", body)
	}
	if !strings.Contains(body, "alice@example.com") {
		t.Fatalf("missing inserted client row, body=%s", body)
	}
	if !strings.Contains(body, "Instagram Ads") {
		t.Fatalf("missing source stage name in csv row, body=%s", body)
	}
}

func TestExportAggregatedCashflowCSV(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	createExportTestSchema(t, db)

	_, _ = db.Exec(`INSERT INTO stages (id,name) VALUES (8,'Webinar')`)
	_, _ = db.Exec(`INSERT INTO clients (id,name,email,phone,source,source_stage_id,status) VALUES (1,'Alice','alice@example.com','123','organic',8,'active')`)
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
	if !strings.Contains(body, "client_source_stage_name") {
		t.Fatalf("missing client_source_stage_name column, body=%s", body)
	}
	if !strings.Contains(body, "m_2026_01") || !strings.Contains(body, "m_2026_02") || !strings.Contains(body, "m_2026_03") {
		t.Fatalf("missing month columns in header, body=%s", body)
	}
	if !strings.Contains(body, "100.00,100.00,0.00") {
		t.Fatalf("expected aggregated monthly amounts, body=%s", body)
	}
}

func TestExportRawContractsCSV(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	createExportTestSchema(t, db)

	_, _ = db.Exec(`INSERT INTO clients (id,name,email,phone,source,status) VALUES (1,'Alice','alice@example.com','123','organic','active')`)
	_, _ = db.Exec(`INSERT INTO contracts (id,client_id,sales_process_id,start_date,end_date,duration_months,revenue_total,payment_frequency,source,created_at,updated_at) VALUES (10,1,100,'2026-01-15','2026-03-20',3,300,'monthly','import','2026-01-01','2026-01-02')`)

	h := &api.Handler{DB: db}
	req := httptest.NewRequest(http.MethodGet, "/api/exports/raw/contracts.csv", nil)
	w := httptest.NewRecorder()

	h.ExportRawContractsCSV(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "id,client_id,sales_process_id,start_date,end_date,duration_months,revenue_total,payment_frequency,source,created_at,updated_at") {
		t.Fatalf("missing contracts csv header, body=%s", body)
	}
	if !strings.Contains(body, ",monthly,import,") {
		t.Fatalf("missing contracts row data, body=%s", body)
	}
}

func TestExportRawCashflowEntriesCSV(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	createExportTestSchema(t, db)

	_, _ = db.Exec(`INSERT INTO cashflow_entries (contract_id,due_date,amount,status,updated_at) VALUES (10,'2026-01-20',100.5,'pending','2026-01-21')`)

	h := &api.Handler{DB: db}
	req := httptest.NewRequest(http.MethodGet, "/api/exports/raw/cashflow_entries.csv", nil)
	w := httptest.NewRecorder()

	h.ExportRawCashflowEntriesCSV(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "id,contract_id,due_date,amount,status,updated_at") {
		t.Fatalf("missing cashflow csv header, body=%s", body)
	}
	if !strings.Contains(body, ",pending,") {
		t.Fatalf("missing cashflow row data, body=%s", body)
	}
}

func TestExportAggregatedCashflowCSV_EmptyContracts(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	createExportTestSchema(t, db)

	h := &api.Handler{DB: db}
	req := httptest.NewRequest(http.MethodGet, "/api/exports/aggregated/cashflow.csv", nil)
	w := httptest.NewRecorder()

	h.ExportAggregatedCashflowCSV(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "client_source_stage_name") {
		t.Fatalf("expected aggregated header in empty export, body=%s", body)
	}
}

func TestExportAggregatedCashflowCSV_InvalidRange(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	createExportTestSchema(t, db)

	_, _ = db.Exec(`INSERT INTO clients (id,name,email,phone,source,status) VALUES (1,'Alice','alice@example.com','123','organic','active')`)
	_, _ = db.Exec(`INSERT INTO contracts (id,client_id,start_date,end_date,duration_months,revenue_total,payment_frequency) VALUES (10,1,'2026-01-15','2026-03-20',3,300,'monthly')`)

	h := &api.Handler{DB: db}

	// invalid from format
	reqBadFrom := httptest.NewRequest(http.MethodGet, "/api/exports/aggregated/cashflow.csv?from=2026/01", nil)
	wBadFrom := httptest.NewRecorder()
	h.ExportAggregatedCashflowCSV(wBadFrom, reqBadFrom)
	if wBadFrom.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid from, got %d", wBadFrom.Code)
	}

	// to before from
	reqBadRange := httptest.NewRequest(http.MethodGet, "/api/exports/aggregated/cashflow.csv?from=2026-03&to=2026-01", nil)
	wBadRange := httptest.NewRecorder()
	h.ExportAggregatedCashflowCSV(wBadRange, reqBadRange)
	if wBadRange.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid range, got %d", wBadRange.Code)
	}
}

func TestExportLegacyCashflowCSV(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	createExportTestSchema(t, db)
	// Add tables needed by the new columns
	for _, stmt := range []string{
		`CREATE TABLE contract_upsells (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			sales_process_id INTEGER, client_id INTEGER,
			upsell_date TEXT, upsell_result TEXT, upsell_revenue REAL,
			previous_contract_id INTEGER, new_contract_id INTEGER,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE comments (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			entity_type TEXT, entity_id INTEGER,
			author TEXT, body TEXT, metadata TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP
		);`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("schema: %v", err)
		}
	}

	_, _ = db.Exec(`INSERT INTO stages (id,name) VALUES (8,'Webinar')`)
	_, _ = db.Exec(`INSERT INTO clients (id,name,status,source,source_stage_id) VALUES (1,'Leila Walder','inactive','paid',8)`)
	_, _ = db.Exec(`INSERT INTO clients (id,name,status,source,source_stage_id) VALUES (2,'Sina Münz','active','organic',NULL)`)
	_, _ = db.Exec(`INSERT INTO contracts (id,client_id,sales_process_id,start_date,end_date,duration_months,revenue_total,payment_frequency) VALUES (1,1,1,'2025-01-01','2025-07-01',6,1800,'monthly')`)
	_, _ = db.Exec(`INSERT INTO contracts (id,client_id,sales_process_id,start_date,end_date,duration_months,revenue_total,payment_frequency) VALUES (2,1,1,'2025-07-01','2026-01-01',6,1800,'monthly')`)
	_, _ = db.Exec(`INSERT INTO contracts (id,client_id,sales_process_id,start_date,end_date,duration_months,revenue_total,payment_frequency) VALUES (3,2,2,'2025-10-01','2026-04-01',6,1800,'monthly')`)
	_, _ = db.Exec(`INSERT INTO cashflow_entries (contract_id,due_date,amount,status) VALUES (1,'2025-01-15',300,'paid')`)
	_, _ = db.Exec(`INSERT INTO cashflow_entries (contract_id,due_date,amount,status) VALUES (1,'2025-02-15',300,'paid')`)
	_, _ = db.Exec(`INSERT INTO cashflow_entries (contract_id,due_date,amount,status) VALUES (2,'2025-07-15',300,'pending')`)
	_, _ = db.Exec(`INSERT INTO cashflow_entries (contract_id,due_date,amount,status) VALUES (3,'2025-11-15',300,'pending')`)
	_, _ = db.Exec(`INSERT INTO contract_upsells (client_id,sales_process_id,upsell_date,upsell_result) VALUES (1,1,'2025-07-01','verlaengerung')`)
	_, _ = db.Exec(`INSERT INTO comments (entity_type,entity_id,body) VALUES ('client',1,'Sehr motiviert')`)
	_, _ = db.Exec(`INSERT INTO comments (entity_type,entity_id,body) VALUES ('client',1,'Zahlt pünktlich')`)

	h := &api.Handler{DB: db}
	req := httptest.NewRequest(http.MethodGet, "/api/exports/legacy/cashflow.csv", nil)
	w := httptest.NewRecorder()
	h.ExportLegacyCashflowCSV(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()

	if !strings.Contains(body, "Januar '25") {
		t.Fatalf("expected German month header 'Januar '25', body=%s", body)
	}
	if !strings.Contains(body, "quelle") || !strings.Contains(body, "quelle_stage") || !strings.Contains(body, "upsells") || !strings.Contains(body, "kommentare") {
		t.Fatalf("missing new header columns, body=%s", body)
	}
	if !strings.Contains(body, "Walder") || !strings.Contains(body, "Leila") {
		t.Fatalf("expected name split into Walder/Leila, body=%s", body)
	}
	if !strings.Contains(body, "3600.00") {
		t.Fatalf("expected CLV 3600.00, body=%s", body)
	}
	if !strings.Contains(body, "01.01.2025") {
		t.Fatalf("expected date formatted as 01.01.2025, body=%s", body)
	}
	if !strings.Contains(body, "paid") || !strings.Contains(body, "Webinar") {
		t.Fatalf("expected source=paid and source_stage=Webinar, body=%s", body)
	}
	if !strings.Contains(body, "1x verlängerung") {
		t.Fatalf("expected upsell result, body=%s", body)
	}
	if !strings.Contains(body, "Sehr motiviert") {
		t.Fatalf("expected comment in output, body=%s", body)
	}
}

func TestExportLegacyCashflowCSV_InvalidParams(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	createExportTestSchema(t, db)

	h := &api.Handler{DB: db}

	// invalid from
	req := httptest.NewRequest(http.MethodGet, "/api/exports/legacy/cashflow.csv?from=badval", nil)
	w := httptest.NewRecorder()
	h.ExportLegacyCashflowCSV(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid from, got %d", w.Code)
	}

	// invalid to
	req2 := httptest.NewRequest(http.MethodGet, "/api/exports/legacy/cashflow.csv?to=badval", nil)
	w2 := httptest.NewRecorder()
	h.ExportLegacyCashflowCSV(w2, req2)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid to, got %d", w2.Code)
	}
}

func TestExportLegacyCashflowCSV_RangeToBeforeFrom(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	createExportTestSchema(t, db)
	for _, stmt := range []string{
		`CREATE TABLE contract_upsells (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			sales_process_id INTEGER, client_id INTEGER,
			upsell_date TEXT, upsell_result TEXT, upsell_revenue REAL,
			previous_contract_id INTEGER, new_contract_id INTEGER,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE comments (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			entity_type TEXT, entity_id INTEGER,
			author TEXT, body TEXT, metadata TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		);`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("schema: %v", err)
		}
	}

	_, _ = db.Exec(`INSERT INTO clients (id,name,source,status) VALUES (1,'Müller Hans','organic','active')`)
	_, _ = db.Exec(`INSERT INTO contracts (client_id,start_date,end_date,duration_months,revenue_total,payment_frequency) VALUES (1,'2026-01-01','2026-03-31',3,300,'monthly')`)

	h := &api.Handler{DB: db}
	req := httptest.NewRequest(http.MethodGet, "/api/exports/legacy/cashflow.csv?from=2026-06&to=2026-01", nil)
	w := httptest.NewRecorder()
	h.ExportLegacyCashflowCSV(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for to before from, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestExportAggregatedCashflowCSV_BadToParam(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	createExportTestSchema(t, db)

	h := &api.Handler{DB: db}
	req := httptest.NewRequest(http.MethodGet, "/api/exports/aggregated/cashflow.csv?to=badval", nil)
	w := httptest.NewRecorder()
	h.ExportAggregatedCashflowCSV(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid to, got %d", w.Code)
	}
}

func TestExportAggregatedCashflowCSV_EmptyClients(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	createExportTestSchema(t, db)

	h := &api.Handler{DB: db}
	req := httptest.NewRequest(http.MethodGet, "/api/exports/aggregated/cashflow.csv", nil)
	w := httptest.NewRecorder()
	h.ExportAggregatedCashflowCSV(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "client_name") {
		t.Fatalf("expected header row even when empty")
	}
}
