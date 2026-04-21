package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestGetDashboardKPIs_InvalidStartDate(t *testing.T) {
	h := &Handler{DB: nil}
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/kpis?start_date=not-a-date", nil)
	w := httptest.NewRecorder()
	h.GetDashboardKPIs(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetDashboardKPIs_InvalidEndDate(t *testing.T) {
	h := &Handler{DB: nil}
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/kpis?end_date=13-99-2025", nil)
	w := httptest.NewRecorder()
	h.GetDashboardKPIs(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetDashboardKPIs_FirstQueryError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(".*").WillReturnError(errTest("upsells query failed"))

	h := &Handler{DB: db}
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/kpis", nil)
	w := httptest.NewRecorder()
	h.GetDashboardKPIs(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

// TestGetDashboardKPIs_Success mocks all 8 QueryRow calls in order and asserts
// the handler returns 200 with all expected KPI keys present.
func TestGetDashboardKPIs_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	// Query 1: upsell aggregates (4 columns)
	mock.ExpectQuery(".*").WillReturnRows(
		sqlmock.NewRows([]string{"renewal_rev", "verl_cnt", "keine_cnt", "quote"}).
			AddRow(1200.0, 3, 2, 60.0),
	)
	// Query 2: totalRevenue
	mock.ExpectQuery(".*").WillReturnRows(
		sqlmock.NewRows([]string{"sum"}).AddRow(4800.0),
	)
	// Query 3: totalCLV (active clients)
	mock.ExpectQuery(".*").WillReturnRows(
		sqlmock.NewRows([]string{"sum"}).AddRow(9600.0),
	)
	// Query 4: newCustomerRevenue
	mock.ExpectQuery(".*").WillReturnRows(
		sqlmock.NewRows([]string{"sum"}).AddRow(3600.0),
	)
	// Query 5: gesamtCLV (all contracts, all time)
	mock.ExpectQuery(".*").WillReturnRows(
		sqlmock.NewRows([]string{"sum"}).AddRow(15000.0),
	)
	// Query 6: activeContractsCount + activeRevenue
	mock.ExpectQuery(".*").WillReturnRows(
		sqlmock.NewRows([]string{"cnt", "rev"}).AddRow(4, 4800.0),
	)
	// Query 7: wonNewCount
	mock.ExpectQuery(".*").WillReturnRows(
		sqlmock.NewRows([]string{"cnt"}).AddRow(3),
	)
	// Query 8: decidedNewCount
	mock.ExpectQuery(".*").WillReturnRows(
		sqlmock.NewRows([]string{"cnt"}).AddRow(5),
	)

	h := &Handler{DB: db}
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/kpis?start_date=2025-01-01&end_date=2025-12-31", nil)
	w := httptest.NewRecorder()
	h.GetDashboardKPIs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var kpis map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&kpis); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Spot-check expected keys
	expectedKeys := []string{
		"renewal_revenue", "new_customer_revenue", "total_revenue",
		"active_revenue", "clv_active_clients", "clv_all_time",
		"active_contracts_count", "won_new_count", "decided_new_count",
		"verlaengerung_count", "keine_verlaengerung_count",
	}
	for _, k := range expectedKeys {
		if _, ok := kpis[k]; !ok {
			t.Errorf("expected key %q in response", k)
		}
	}

	if v, _ := kpis["active_revenue"].(float64); v != 4800.0/1.19 {
		t.Errorf("expected active_revenue=4800, got %v", v)
	}
	if v, _ := kpis["clv_all_time"].(float64); v != 15000.0/1.19 {
		t.Errorf("expected clv_all_time=15000, got %v", v)
	}

	// closing_rate_new = 3/5 * 100 = 60.0
	if cr, ok := kpis["closing_rate_new"].(float64); !ok || cr != 60.0 {
		t.Errorf("expected closing_rate_new=60.0, got %v", kpis["closing_rate_new"])
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
