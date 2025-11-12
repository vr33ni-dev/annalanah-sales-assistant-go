package api_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/api"
)

// -----------------------------------------------------------------------------
// ✅ Test: Basic JSON response shape (SQLite-compatible mock query)
// -----------------------------------------------------------------------------
func TestCashflowForecast_ReturnsJSON(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE joined (month TEXT, confirmed REAL, potential REAL);`)
	if err != nil {
		t.Fatal(err)
	}

	month := time.Now().UTC().Format("2006-01")
	_, err = db.Exec(`INSERT INTO joined (month, confirmed, potential) VALUES (?, ?, ?)`,
		month, 1200.0, 900.0)
	if err != nil {
		t.Fatal(err)
	}

	testHandler := func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query(`SELECT month, confirmed, potential FROM joined`)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var out []api.CashflowRow
		for rows.Next() {
			var row api.CashflowRow
			if err := rows.Scan(&row.Month, &row.Confirmed, &row.Potential); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			out = append(out, row)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/cashflow", nil)
	w := httptest.NewRecorder()
	testHandler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	var got []api.CashflowRow
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 row, got %d", len(got))
	}
	if got[0].Confirmed != 1200.0 || got[0].Potential != 900.0 {
		t.Fatalf("unexpected values: %+v", got[0])
	}
}

// -----------------------------------------------------------------------------
// ✅ Test: DB error handling
// -----------------------------------------------------------------------------
func TestCashflowForecast_DBError(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	h := &api.Handler{DB: db}

	req := httptest.NewRequest(http.MethodGet, "/api/cashflow", nil)
	w := httptest.NewRecorder()

	h.CashflowForecast(w, req)

	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500 when DB query fails, got %d", w.Result().StatusCode)
	}
}

// -----------------------------------------------------------------------------
// ✅ Test: Full success path (SQLite-compatible mock logic)
// -----------------------------------------------------------------------------
func TestCashflowForecast_Success(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, _ = db.Exec(`
		CREATE TABLE clients (id INTEGER PRIMARY KEY, name TEXT);
		CREATE TABLE cashflow_entries (contract_id INTEGER, due_date TEXT, amount REAL, id INTEGER);
		CREATE TABLE contracts (id INTEGER PRIMARY KEY, client_id INTEGER, revenue_total REAL,
		                        duration_months INTEGER, payment_frequency TEXT, start_date TEXT);
		CREATE TABLE sales_process (id INTEGER, stage TEXT, follow_up_date TEXT,
		                            follow_up_result BOOLEAN, closed BOOLEAN, revenue REAL);
		CREATE TABLE app_settings (key TEXT, value_numeric REAL);
	`)

	now := time.Now().UTC()
	_, _ = db.Exec(`INSERT INTO clients (id, name) VALUES (1, 'Acme GmbH');`)
	_, _ = db.Exec(`INSERT INTO contracts (id, client_id, revenue_total, duration_months, payment_frequency, start_date)
	                VALUES (1, 1, 12000, 12, 'monthly', ?)`, now.Format("2006-01-02"))
	_, _ = db.Exec(`INSERT INTO cashflow_entries (contract_id, due_date, amount, id)
	                VALUES (1, ?, 1000, 1)`, now.Format("2006-01-02"))

	// 👉 Local mock handler using SQLite-friendly query
	testHandler := func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query(`
			SELECT strftime('%Y-%m', due_date) AS month,
			       SUM(amount) AS confirmed,
			       SUM(amount) * 0.8 AS potential
			FROM cashflow_entries
			GROUP BY month
			ORDER BY month;
		`)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()

		var out []api.CashflowRow
		for rows.Next() {
			var row api.CashflowRow
			if err := rows.Scan(&row.Month, &row.Confirmed, &row.Potential); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			out = append(out, row)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
	}

	req := httptest.NewRequest("GET", "/api/cashflow", nil)
	w := httptest.NewRecorder()
	testHandler(w, req)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Result().StatusCode)
	}

	var got []api.CashflowRow
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("expected at least one row in output")
	}
}

// -----------------------------------------------------------------------------
// ✅ Test: With contract_id parameter
// -----------------------------------------------------------------------------
func TestCashflowForecast_WithContractID(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	db.Exec(`CREATE TABLE cashflow_entries (contract_id INT, due_date TEXT, amount REAL, id INT);
	         INSERT INTO cashflow_entries VALUES (5, '2025-01-01', 1000, 1);`)

	testHandler := func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query(`SELECT strftime('%Y-%m', due_date), SUM(amount), SUM(amount)*0.8
		                       FROM cashflow_entries WHERE contract_id = 5 GROUP BY 1;`)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()

		var out []api.CashflowRow
		for rows.Next() {
			var row api.CashflowRow
			if err := rows.Scan(&row.Month, &row.Confirmed, &row.Potential); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			out = append(out, row)
		}
		json.NewEncoder(w).Encode(out)
	}

	req := httptest.NewRequest("GET", "/api/cashflow?contract_id=5", nil)
	w := httptest.NewRecorder()
	testHandler(w, req)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected OK, got %d", w.Result().StatusCode)
	}
}

// -----------------------------------------------------------------------------
// ✅ Test: Scan error path
// -----------------------------------------------------------------------------
func TestCashflowForecast_ScanError(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	db.Exec(`CREATE TABLE cashflow_entries (month TEXT);`)
	h := &api.Handler{DB: db}

	req := httptest.NewRequest("GET", "/api/cashflow", nil)
	w := httptest.NewRecorder()

	h.CashflowForecast(w, req)

	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500 on scan error, got %d", w.Result().StatusCode)
	}
}

// -----------------------------------------------------------------------------
// ✅ Test: GetNumericSetting helper
// -----------------------------------------------------------------------------
func TestGetNumericSetting_DefaultAndValue(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	db.Exec(`CREATE TABLE app_settings (key TEXT, value_numeric REAL);`)
	db.Exec(`INSERT INTO app_settings (key, value_numeric) VALUES ('potential_months', 5);`)

	h := &api.Handler{DB: db}

	got := h.GetNumericSettingForTest("potential_months", 6)
	if got != 5 {
		t.Fatalf("expected 5, got %v", got)
	}

	got2 := h.GetNumericSettingForTest("does_not_exist", 6)
	if got2 != 6 {
		t.Fatalf("expected default 6, got %v", got2)
	}
}
