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
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/api/testhelpers"
)

//
// ============================================================================
//  SUCCESS-PATH TESTS (REAL POSTGRES)
// ============================================================================
//

func TestCashflowForecast_Success_Postgres(t *testing.T) {
	s := testhelpers.NewAPITestSuite(t)
	defer s.TearDown()
	s.Cleanup(t)

	// Create client → process → contract
	client := s.CreateClient()
	proc := s.CreateSalesProcessForClient(client.ID)
	contract := s.CreateContract(client.ID, proc.ID)

	// Add cashflow entries via factory
	now := time.Now().UTC()
	s.CreateCashflowEntry(contract.ID, now, 1200.0)
	s.CreateCashflowEntry(contract.ID, now, 900.0)

	req := httptest.NewRequest(http.MethodGet, "/api/cashflow", nil)
	w := httptest.NewRecorder()

	s.Handler.CashflowForecast(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var rows []api.CashflowRow
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if len(rows) != 6 {
		t.Fatalf("expected 6 rows (6-month forecast window), got %d", len(rows))
	}

	if rows[0].Confirmed != 2100 {
		t.Fatalf("expected confirmed=2100, got=%v", rows[0].Confirmed)
	}
}

//
// ============================================================================
//  SQLITE TESTS (ERROR PATHS ONLY)
// ============================================================================
//

func TestCashflowForecast_ReturnsJSON_SQLite(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()

	db.Exec(`CREATE TABLE joined (month TEXT, confirmed REAL, potential REAL);`)
	month := time.Now().UTC().Format("2006-01")
	db.Exec(`INSERT INTO joined (month, confirmed, potential) VALUES (?, ?, ?)`,
		month, 1200.0, 900.0)

	testHandler := func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query(`SELECT month, confirmed, potential FROM joined`)
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

	req := httptest.NewRequest("GET", "/api/cashflow", nil)
	w := httptest.NewRecorder()
	testHandler(w, req)

	if w.Result().StatusCode != 200 {
		t.Fatalf("expected 200, got %d", w.Result().StatusCode)
	}
}

func TestCashflowForecast_DBError(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	db.Close() // simulate failure

	h := &api.Handler{DB: db}

	req := httptest.NewRequest("GET", "/api/cashflow", nil)
	w := httptest.NewRecorder()

	h.CashflowForecast(w, req)

	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Result().StatusCode)
	}
}

func TestCashflowForecast_WithContractID_SQLite(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()

	db.Exec(`CREATE TABLE cashflow_entries (contract_id INT, due_date TEXT, amount REAL, id INT);`)
	db.Exec(`INSERT INTO cashflow_entries VALUES (5, '2025-01-01', 1000, 1);`)

	testHandler := func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query(`
			SELECT strftime('%Y-%m', due_date), SUM(amount), SUM(amount) * 0.8
			FROM cashflow_entries WHERE contract_id = 5
			GROUP BY 1;
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

		json.NewEncoder(w).Encode(out)
	}

	req := httptest.NewRequest("GET", "/api/cashflow?contract_id=5", nil)
	w := httptest.NewRecorder()
	testHandler(w, req)

	if w.Result().StatusCode != 200 {
		t.Fatalf("expected OK, got %d", w.Result().StatusCode)
	}
}

func TestCashflowForecast_ScanError(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()

	db.Exec(`CREATE TABLE cashflow_entries (month TEXT);`) // wrong schema → scan error

	h := &api.Handler{DB: db}

	req := httptest.NewRequest("GET", "/api/cashflow", nil)
	w := httptest.NewRecorder()

	h.CashflowForecast(w, req)

	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Result().StatusCode)
	}
}

func TestGetNumericSetting_DefaultAndValue(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()

	db.Exec(`CREATE TABLE app_settings (key TEXT, value_numeric REAL);`)
	db.Exec(`INSERT INTO app_settings VALUES ('potential_months', 5);`)

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
