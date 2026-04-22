package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestGetContractsInRange_InvalidType(t *testing.T) {
	h := &Handler{DB: nil}
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/contracts-in-range?type=foo", nil)
	w := httptest.NewRecorder()

	h.GetContractsInRange(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetContractsInRange_InvalidStartDate(t *testing.T) {
	h := &Handler{DB: nil}
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/contracts-in-range?type=neukunden&start_date=invalid", nil)
	w := httptest.NewRecorder()

	h.GetContractsInRange(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetContractsInRange_InvalidEndDate(t *testing.T) {
	h := &Handler{DB: nil}
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/contracts-in-range?type=verlaengerung&end_date=31-12-2025", nil)
	w := httptest.NewRecorder()

	h.GetContractsInRange(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetContractsInRange_QueryError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("FROM contracts c").
		WillReturnError(errTest("contracts in range query failed"))

	h := &Handler{DB: db}
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/contracts-in-range?type=neukunden", nil)
	w := httptest.NewRecorder()

	h.GetContractsInRange(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetContractsInRange_SuccessNeukunden(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	rows := sqlmock.NewRows([]string{"id", "client_id", "name", "start_date", "end_date", "revenue_total"}).
		AddRow(101, 11, "Alice Beispiel", "2025-01-10", "2025-04-10", 1190.0).
		AddRow(102, 12, "Bob Beispiel", "2025-02-01", nil, 2380.0)

	mock.ExpectQuery("FROM contracts c").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(rows)

	h := &Handler{DB: db}
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/contracts-in-range?type=neukunden&start_date=2025-01-01&end_date=2025-12-31", nil)
	w := httptest.NewRecorder()

	h.GetContractsInRange(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var got []map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(got))
	}

	if got[0]["contract_id"].(float64) != 101 {
		t.Fatalf("expected contract_id 101, got %v", got[0]["contract_id"])
	}
	if got[0]["revenue_netto"].(float64) != 1000.0 {
		t.Fatalf("expected revenue_netto 1000.0, got %v", got[0]["revenue_netto"])
	}
	if got[0]["monetary_mode"] != "netto" {
		t.Fatalf("expected monetary_mode netto, got %v", got[0]["monetary_mode"])
	}
	if got[1]["revenue_netto"].(float64) != 2000.0 {
		t.Fatalf("expected revenue_netto 2000.0, got %v", got[1]["revenue_netto"])
	}
	if got[1]["monetary_mode"] != "netto" {
		t.Fatalf("expected monetary_mode netto, got %v", got[1]["monetary_mode"])
	}
	if _, ok := got[1]["end_date"]; ok {
		t.Fatalf("expected end_date to be omitted for NULL end_date")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestGetContractsInRange_SuccessVerlaengerungFallback(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	rows := sqlmock.NewRows([]string{"id", "client_id", "name", "start_date", "end_date", "revenue"}).
		AddRow(201, 21, "Carla Kunde", "2025-03-15", "2025-09-15", 595.0)

	mock.ExpectQuery("COALESCE\\(cu.upsell_revenue, c.revenue_total\\)").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(rows)

	h := &Handler{DB: db}
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/contracts-in-range?type=verlaengerung&start_date=2025-01-01&end_date=2025-12-31", nil)
	w := httptest.NewRecorder()

	h.GetContractsInRange(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var got []map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("expected 1 row, got %d", len(got))
	}

	if got[0]["contract_id"].(float64) != 201 {
		t.Fatalf("expected contract_id 201, got %v", got[0]["contract_id"])
	}
	if got[0]["revenue_netto"].(float64) != 500.0 {
		t.Fatalf("expected revenue_netto 500.0, got %v", got[0]["revenue_netto"])
	}
	if got[0]["monetary_mode"] != "netto" {
		t.Fatalf("expected monetary_mode netto, got %v", got[0]["monetary_mode"])
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

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
		"monetary_mode",
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
	if mm, ok := kpis["monetary_mode"].(string); !ok || mm != "netto" {
		t.Errorf("expected monetary_mode=netto, got %v", kpis["monetary_mode"])
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// ─── Error path tests for each subsequent QueryRow in GetDashboardKPIs ────────

func TestGetDashboardKPIs_TotalRevenueQueryError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	h := &Handler{DB: db}

	// Query 1 succeeds
	mock.ExpectQuery(".*").WillReturnRows(
		sqlmock.NewRows([]string{"rr", "vc", "kc", "vq"}).AddRow(0.0, 0, 0, nil))
	// Query 2 fails
	mock.ExpectQuery(".*").WillReturnError(errTest("total revenue error"))

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/kpis", nil)
	w := httptest.NewRecorder()
	h.GetDashboardKPIs(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetDashboardKPIs_CLVActiveClientsQueryError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	h := &Handler{DB: db}

	mock.ExpectQuery(".*").WillReturnRows(sqlmock.NewRows([]string{"rr", "vc", "kc", "vq"}).AddRow(0.0, 0, 0, nil))
	mock.ExpectQuery(".*").WillReturnRows(sqlmock.NewRows([]string{"total"}).AddRow(0.0))
	// Query 3 fails
	mock.ExpectQuery(".*").WillReturnError(errTest("clv active error"))

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/kpis", nil)
	w := httptest.NewRecorder()
	h.GetDashboardKPIs(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetDashboardKPIs_NewCustomerRevenueQueryError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	h := &Handler{DB: db}

	mock.ExpectQuery(".*").WillReturnRows(sqlmock.NewRows([]string{"rr", "vc", "kc", "vq"}).AddRow(0.0, 0, 0, nil))
	mock.ExpectQuery(".*").WillReturnRows(sqlmock.NewRows([]string{"total"}).AddRow(0.0))
	mock.ExpectQuery(".*").WillReturnRows(sqlmock.NewRows([]string{"clv"}).AddRow(0.0))
	// Query 4 fails
	mock.ExpectQuery(".*").WillReturnError(errTest("new customer revenue error"))

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/kpis", nil)
	w := httptest.NewRecorder()
	h.GetDashboardKPIs(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetDashboardKPIs_GesamtCLVQueryError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	h := &Handler{DB: db}

	mock.ExpectQuery(".*").WillReturnRows(sqlmock.NewRows([]string{"rr", "vc", "kc", "vq"}).AddRow(0.0, 0, 0, nil))
	mock.ExpectQuery(".*").WillReturnRows(sqlmock.NewRows([]string{"total"}).AddRow(0.0))
	mock.ExpectQuery(".*").WillReturnRows(sqlmock.NewRows([]string{"clv"}).AddRow(0.0))
	mock.ExpectQuery(".*").WillReturnRows(sqlmock.NewRows([]string{"ncr"}).AddRow(0.0))
	// Query 5 fails
	mock.ExpectQuery(".*").WillReturnError(errTest("gesamt clv error"))

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/kpis", nil)
	w := httptest.NewRecorder()
	h.GetDashboardKPIs(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetDashboardKPIs_ActiveContractsQueryError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	h := &Handler{DB: db}

	mock.ExpectQuery(".*").WillReturnRows(sqlmock.NewRows([]string{"rr", "vc", "kc", "vq"}).AddRow(0.0, 0, 0, nil))
	mock.ExpectQuery(".*").WillReturnRows(sqlmock.NewRows([]string{"total"}).AddRow(0.0))
	mock.ExpectQuery(".*").WillReturnRows(sqlmock.NewRows([]string{"clv"}).AddRow(0.0))
	mock.ExpectQuery(".*").WillReturnRows(sqlmock.NewRows([]string{"ncr"}).AddRow(0.0))
	mock.ExpectQuery(".*").WillReturnRows(sqlmock.NewRows([]string{"gesamt"}).AddRow(0.0))
	// Query 6 fails
	mock.ExpectQuery(".*").WillReturnError(errTest("active contracts error"))

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/kpis", nil)
	w := httptest.NewRecorder()
	h.GetDashboardKPIs(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetDashboardKPIs_WonNewQueryError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	h := &Handler{DB: db}

	mock.ExpectQuery(".*").WillReturnRows(sqlmock.NewRows([]string{"rr", "vc", "kc", "vq"}).AddRow(0.0, 0, 0, nil))
	mock.ExpectQuery(".*").WillReturnRows(sqlmock.NewRows([]string{"total"}).AddRow(0.0))
	mock.ExpectQuery(".*").WillReturnRows(sqlmock.NewRows([]string{"clv"}).AddRow(0.0))
	mock.ExpectQuery(".*").WillReturnRows(sqlmock.NewRows([]string{"ncr"}).AddRow(0.0))
	mock.ExpectQuery(".*").WillReturnRows(sqlmock.NewRows([]string{"gesamt"}).AddRow(0.0))
	mock.ExpectQuery(".*").WillReturnRows(sqlmock.NewRows([]string{"cnt", "rev"}).AddRow(0, 0.0))
	// Query 7 fails
	mock.ExpectQuery(".*").WillReturnError(errTest("won new error"))

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/kpis", nil)
	w := httptest.NewRecorder()
	h.GetDashboardKPIs(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetDashboardKPIs_DecidedNewQueryError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	h := &Handler{DB: db}

	mock.ExpectQuery(".*").WillReturnRows(sqlmock.NewRows([]string{"rr", "vc", "kc", "vq"}).AddRow(0.0, 0, 0, nil))
	mock.ExpectQuery(".*").WillReturnRows(sqlmock.NewRows([]string{"total"}).AddRow(0.0))
	mock.ExpectQuery(".*").WillReturnRows(sqlmock.NewRows([]string{"clv"}).AddRow(0.0))
	mock.ExpectQuery(".*").WillReturnRows(sqlmock.NewRows([]string{"ncr"}).AddRow(0.0))
	mock.ExpectQuery(".*").WillReturnRows(sqlmock.NewRows([]string{"gesamt"}).AddRow(0.0))
	mock.ExpectQuery(".*").WillReturnRows(sqlmock.NewRows([]string{"cnt", "rev"}).AddRow(0, 0.0))
	mock.ExpectQuery(".*").WillReturnRows(sqlmock.NewRows([]string{"won"}).AddRow(0))
	// Query 8 fails
	mock.ExpectQuery(".*").WillReturnError(errTest("decided new error"))

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/kpis", nil)
	w := httptest.NewRecorder()
	h.GetDashboardKPIs(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetMonthlyKPIs_InvalidYear(t *testing.T) {
	h := &Handler{DB: nil}
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/monthly-kpis?year=invalid", nil)
	w := httptest.NewRecorder()

	h.GetMonthlyKPIs(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetMonthlyKPIs_SuccessIncludesMonetaryModeAndConvertedRevenue(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	// Query 1: revenue per month (Brutto)
	mock.ExpectQuery("EXTRACT\\(MONTH FROM c.start_date\\)::int AS month").
		WithArgs(2026).
		WillReturnRows(
			sqlmock.NewRows([]string{"month", "revenue"}).
				AddRow(1, 1190.0).
				AddRow(2, 2380.0),
		)

	// Query 2: won deals per month
	mock.ExpectQuery("EXTRACT\\(MONTH FROM cl.completed_at\\)::int AS month").
		WithArgs(2026).
		WillReturnRows(
			sqlmock.NewRows([]string{"month", "won_count"}).
				AddRow(1, 3),
		)

	// Query 3: decided deals per month
	mock.ExpectQuery("COUNT\\(\\*\\) AS decided_count").
		WithArgs(2026).
		WillReturnRows(
			sqlmock.NewRows([]string{"month", "decided_count"}).
				AddRow(1, 4),
		)

	h := &Handler{DB: db}
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/monthly-kpis?year=2026", nil)
	w := httptest.NewRecorder()

	h.GetMonthlyKPIs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var out []map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if len(out) != 12 {
		t.Fatalf("expected 12 months, got %d", len(out))
	}

	jan := out[0]
	if jan["month"].(float64) != 1 {
		t.Fatalf("expected month 1 at index 0, got %v", jan["month"])
	}
	if jan["monetary_mode"] != "netto" {
		t.Fatalf("expected monetary_mode netto, got %v", jan["monetary_mode"])
	}
	if jan["revenue"].(float64) != 1000.0 {
		t.Fatalf("expected january revenue netto 1000.0, got %v", jan["revenue"])
	}
	if jan["closed_deals"].(float64) != 3 {
		t.Fatalf("expected january closed_deals 3, got %v", jan["closed_deals"])
	}
	if jan["closing_rate"].(float64) != 75.0 {
		t.Fatalf("expected january closing_rate 75.0, got %v", jan["closing_rate"])
	}

	feb := out[1]
	if feb["revenue"].(float64) != 2000.0 {
		t.Fatalf("expected february revenue netto 2000.0, got %v", feb["revenue"])
	}
	if feb["monetary_mode"] != "netto" {
		t.Fatalf("expected february monetary_mode netto, got %v", feb["monetary_mode"])
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
