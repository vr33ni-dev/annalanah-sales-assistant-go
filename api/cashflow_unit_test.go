package api_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
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

func TestCashflowMetrics_Handler(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to open sqlmock: %v", err)
	}
	defer db.Close()

	h := &api.Handler{DB: db}

	// Expect YTD paid sum query
	mock.ExpectQuery("SELECT COALESCE\\(SUM\\(amount\\) FILTER").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(1200.0))

	// Expect next-3-months confirmed query and return 3 months
	mock.ExpectQuery("SELECT ym AS month, SUM\\(amt\\)::numeric AS confirmed").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"month", "confirmed"}).
			AddRow("2026-02", 400.0).
			AddRow("2026-03", 300.0).
			AddRow("2026-04", 200.0))

	req := httptest.NewRequest("GET", "/api/cashflow/dashboard", nil)
	w := httptest.NewRecorder()

	h.CashflowMetrics(w, req)

	if w.Code != 200 {
		t.Fatalf("unexpected status: %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}

	// Basic assertions
	if _, ok := resp["avg_monthly_ytd"]; !ok {
		t.Fatalf("missing avg_monthly_ytd")
	}
	if _, ok := resp["confirmed_next3"]; !ok {
		t.Fatalf("missing confirmed_next3")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestCashflowMetrics_ExcludesNotPaid_and_NoNext3(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to open sqlmock: %v", err)
	}
	defer db.Close()

	h := &api.Handler{DB: db}

	// YTD query: return 1000
	mock.ExpectQuery("SELECT COALESCE\\(SUM\\(amount\\) FILTER").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(1000.0))

	// Next-3-months confirmed query: return no rows
	mock.ExpectQuery("SELECT ym AS month, SUM\\(amt\\)::numeric AS confirmed").
		WillReturnRows(sqlmock.NewRows([]string{"month", "confirmed"}))

	req := httptest.NewRequest("GET", "/api/cashflow/dashboard", nil)
	w := httptest.NewRecorder()

	h.CashflowMetrics(w, req)

	if w.Code != 200 {
		t.Fatalf("unexpected status: %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}

	// ytd_paid_amount should be 1000
	if resp["ytd_paid_amount"] == nil {
		t.Fatalf("missing ytd_paid_amount")
	}
	if resp["ytd_paid_amount"].(float64) != 1000.0 {
		t.Fatalf("expected ytd_paid_amount 1000, got %v", resp["ytd_paid_amount"])
	}

	// confirmed_next3 may be null or an empty array when there are no months
	if resp["confirmed_next3"] != nil {
		if list, ok := resp["confirmed_next3"].([]interface{}); !ok {
			t.Fatalf("confirmed_next3 present but not an array: %v", resp["confirmed_next3"])
		} else if len(list) != 0 {
			t.Fatalf("expected 0 items in confirmed_next3, got %d", len(list))
		}
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestCashflowMetrics_AvgMonthlyYTD_Calculation(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to open sqlmock: %v", err)
	}
	defer db.Close()

	h := &api.Handler{DB: db}

	// YTD query: return 1200
	mock.ExpectQuery("SELECT COALESCE\\(SUM\\(amount\\) FILTER").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(1200.0))

	// Next-3-months confirmed query: return 2 months of amounts
	mock.ExpectQuery("SELECT ym AS month, SUM\\(amt\\)::numeric AS confirmed").
		WillReturnRows(sqlmock.NewRows([]string{"month", "confirmed"}).
			AddRow("2026-02", 400.0).
			AddRow("2026-03", 300.0))

	req := httptest.NewRequest("GET", "/api/cashflow/dashboard", nil)
	w := httptest.NewRecorder()

	h.CashflowMetrics(w, req)

	if w.Code != 200 {
		t.Fatalf("unexpected status: %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}

	// months_elapsed_ytd should be > 0 and avg_monthly_ytd == 1200 / months_elapsed
	monthsElapsed, ok := resp["months_elapsed_ytd"].(float64)
	if !ok || monthsElapsed <= 0 {
		t.Fatalf("invalid months_elapsed_ytd: %v", resp["months_elapsed_ytd"])
	}
	avg, ok := resp["avg_monthly_ytd"].(float64)
	if !ok {
		t.Fatalf("missing avg_monthly_ytd")
	}
	expected := 1200.0 / monthsElapsed
	if avg != expected {
		t.Fatalf("expected avg_monthly_ytd %v, got %v", expected, avg)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
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
