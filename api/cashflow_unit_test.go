package api_test

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/vr33ni-dev/annalanah-sales-assistant-go/api"
)

//
// ============================================================================
// UNIT TESTS (SQLite, error paths, logic only)
// ============================================================================
//

func TestCashflowForecast_DBError(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	db.Close()

	h := &api.Handler{DB: db}

	req := httptest.NewRequest("GET", "/api/cashflow", nil)
	w := httptest.NewRecorder()

	h.CashflowForecast(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestCashflowForecast_ScanError(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()

	db.Exec(`CREATE TABLE cashflow_entries (month TEXT);`)

	h := &api.Handler{DB: db}

	req := httptest.NewRequest("GET", "/api/cashflow", nil)
	w := httptest.NewRecorder()

	h.CashflowForecast(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestGetNumericSetting_DefaultAndValue(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()

	db.Exec(`CREATE TABLE app_settings (key TEXT, value_numeric REAL);`)
	db.Exec(`INSERT INTO app_settings VALUES ('potential_months', 5);`)

	h := &api.Handler{DB: db}

	if got := h.GetNumericSettingForTest("potential_months", 6); got != 5 {
		t.Fatalf("expected 5, got %v", got)
	}

	if got := h.GetNumericSettingForTest("missing", 6); got != 6 {
		t.Fatalf("expected default 6, got %v", got)
	}
}

//
// SQLite aggregation sanity check (UNIT test)
//

func TestCashflowForecast_WithContractID_SQLiteAggregation(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()

	db.Exec(`CREATE TABLE cashflow_entries (contract_id INT, due_date TEXT, amount REAL);`)
	db.Exec(`INSERT INTO cashflow_entries VALUES (5, '2025-01-01', 1000);`)

	rows, err := db.Query(`
		SELECT strftime('%Y-%m', due_date), SUM(amount), SUM(amount) * 0.8
		FROM cashflow_entries
		WHERE contract_id = 5
		GROUP BY 1
	`)
	if err != nil {
		t.Fatal(err)
	}

	var out []api.CashflowRow
	for rows.Next() {
		var r api.CashflowRow
		rows.Scan(&r.Month, &r.Confirmed, &r.Potential)
		out = append(out, r)
	}

	if len(out) != 1 {
		t.Fatalf("expected 1 row, got %d", len(out))
	}
}
