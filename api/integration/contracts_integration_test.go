package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/api"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/api/testhelpers"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/api/testhelpers/factory"
)

// ----------------------------------------------------
// LIST CONTRACTS (success – real DB)
// ----------------------------------------------------
func TestListContracts_Integration(t *testing.T) {
	suite := factory.NewSuiteFromTestDB(t, testDB)
	handler := &api.Handler{DB: suite.DB.DB}

	testhelpers.TruncateAll(t, suite.DB)

	// Arrange: client → sales process → contract
	client := suite.CreateClient()
	sp := suite.CreateSalesProcessForClient(client.ID)
	contract := suite.CreateContract(client.ID, sp.ID)

	// One paid cashflow entry
	suite.CreatePaidCashflow(contract.ID, time.Now().UTC(), 500)

	// Act
	req := httptest.NewRequest(http.MethodGet, "/api/contracts", nil)
	w := httptest.NewRecorder()

	handler.ListContracts(w, req)

	// Assert HTTP
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var out []api.ContractResponse
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}

	if len(out) != 1 {
		t.Fatalf("expected 1 contract, got %d", len(out))
	}

	got := out[0]

	if got.ID != contract.ID {
		t.Fatalf("expected contract ID %d, got %d", contract.ID, got.ID)
	}

}

func TestListContracts_Integration_HidesExpiredByDefault(t *testing.T) {
	suite := factory.NewSuiteFromTestDB(t, testDB)
	handler := &api.Handler{DB: suite.DB.DB}

	testhelpers.TruncateAll(t, suite.DB)

	clientA := suite.CreateClient()
	spA := suite.CreateSalesProcessForClient(clientA.ID)
	activeContract := suite.CreateContract(clientA.ID, spA.ID)

	clientB := suite.CreateClient()
	spB := suite.CreateSalesProcessForClient(clientB.ID)
	expiredContract := suite.CreateContract(clientB.ID, spB.ID)

	_, err := suite.DB.DB.Exec(`
		UPDATE contracts
		SET end_date = CASE
			WHEN id = $1 THEN CURRENT_DATE + INTERVAL '5 days'
			WHEN id = $2 THEN CURRENT_DATE - INTERVAL '1 day'
		END
		WHERE id IN ($1, $2)
	`, activeContract.ID, expiredContract.ID)
	if err != nil {
		t.Fatalf("failed to set contract end dates: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/contracts", nil)
	w := httptest.NewRecorder()

	handler.ListContracts(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}

	var out []api.ContractResponse
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}

	if len(out) != 1 {
		t.Fatalf("expected 1 visible contract, got %d", len(out))
	}

	if out[0].ID != activeContract.ID {
		t.Fatalf("expected active contract ID %d, got %d", activeContract.ID, out[0].ID)
	}
}

func TestCreateContract_Integration(t *testing.T) {
	suite := factory.NewSuiteFromTestDB(t, testDB)
	handler := &api.Handler{DB: suite.DB.DB}

	testhelpers.TruncateAll(t, suite.DB)

	client := suite.CreateClient()
	sp := suite.CreateSalesProcessForClient(client.ID)

	body := api.Contract{
		ClientID:       client.ID,
		SalesProcessID: &sp.ID,
		StartDate:      "2025-01-01",
		DurationMonths: 12,
		RevenueTotal:   1200,
		PaymentFreq:    "monthly",
	}

	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/contracts", bytes.NewReader(b))
	w := httptest.NewRecorder()

	handler.CreateContract(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}

	var out api.Contract
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if out.ID == 0 {
		t.Fatalf("expected generated contract ID")
	}

	// 🔎 Verify projection rows
	rows, err := suite.DB.DB.Query(`
		SELECT due_date, amount
		FROM cashflow_entries
		WHERE contract_id = $1
		ORDER BY due_date
	`, out.ID)
	if err != nil {
		t.Fatalf("query projection rows failed: %v", err)
	}
	defer rows.Close()

	var count int
	var firstDue time.Time
	var lastDue time.Time
	var amount float64

	for rows.Next() {
		var due time.Time
		if err := rows.Scan(&due, &amount); err != nil {
			t.Fatalf("scan failed: %v", err)
		}
		if count == 0 {
			firstDue = due
		}
		lastDue = due
		count++
	}

	// Expect 12 rows (full-period-fit start dates)
	if count != 12 {
		t.Fatalf("expected 12 projection rows, got %d", count)
	}

	expectedStart, _ := time.Parse("2006-01-02", "2025-01-01")
	expectedEnd := expectedStart.AddDate(0, 11, 0)

	if !firstDue.Equal(expectedStart) {
		t.Fatalf("expected first due_date %v, got %v", expectedStart, firstDue)
	}

	if !lastDue.Equal(expectedEnd) {
		t.Fatalf("expected last due_date %v, got %v", expectedEnd, lastDue)
	}

	// 1200 / 12 periods
	expectedAmount := 1200.0 / 12.0

	if amount != expectedAmount {
		t.Fatalf("expected per-period amount %v, got %v", expectedAmount, amount)
	}
}

func TestUpdateContract_Integration(t *testing.T) {
	suite := factory.NewSuiteFromTestDB(t, testDB)

	// Always reset data at the start of the test
	testhelpers.TruncateAll(t, suite.DB)

	handler := &api.Handler{DB: suite.DB.DB}

	client := suite.CreateClient()
	sp := suite.CreateSalesProcessForClient(client.ID)
	contract := suite.CreateContract(client.ID, sp.ID)

	update := api.UpdateContractRequest{
		StartDate:      "2025-02-01",
		DurationMonths: 24,
		RevenueTotal:   2400,
		PaymentFreq:    "monthly",
	}

	b, _ := json.Marshal(update)

	req := httptest.NewRequest(
		http.MethodPatch,
		fmt.Sprintf("/api/contracts/%d", contract.ID),
		bytes.NewReader(b),
	)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()

	// Route through chi (recommended)
	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		r.Route("/contracts", func(r chi.Router) {
			r.Patch("/{id}", handler.UpdateContract)
		})
	})

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d (%s)", w.Code, w.Body.String())
	}

	var revenue float64
	testhelpers.MustQueryRow(
		t,
		suite.DB.DB,
		`SELECT revenue_total FROM contracts WHERE id=$1`,
		contract.ID,
	).Scan(&revenue)

	if revenue != 2400 {
		t.Fatalf("expected revenue_total=2400, got %v", revenue)
	}
}

func TestUpdateContract_InvalidDate_Integration(t *testing.T) {
	suite := factory.NewSuiteFromTestDB(t, testDB)

	testhelpers.TruncateAll(t, suite.DB)

	handler := &api.Handler{DB: suite.DB.DB}

	client := suite.CreateClient()
	sp := suite.CreateSalesProcessForClient(client.ID)
	contract := suite.CreateContract(client.ID, sp.ID)

	update := api.UpdateContractRequest{
		StartDate:      "02-01-2025", // ❌ invalid
		DurationMonths: 12,
		RevenueTotal:   1200,
		PaymentFreq:    "monthly",
	}

	b, _ := json.Marshal(update)

	req := httptest.NewRequest(
		http.MethodPatch,
		fmt.Sprintf("/api/contracts/%d", contract.ID),
		bytes.NewReader(b),
	)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.Itoa(contract.ID))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()

	handler.UpdateContract(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestProjection_Quarterly(t *testing.T) {
	suite := factory.NewSuiteFromTestDB(t, testDB)
	testhelpers.TruncateAll(t, suite.DB)

	handler := &api.Handler{DB: suite.DB.DB}

	client := suite.CreateClient()
	sp := suite.CreateSalesProcessForClient(client.ID)

	body := api.Contract{
		ClientID:       client.ID,
		SalesProcessID: &sp.ID,
		StartDate:      "2025-01-01",
		DurationMonths: 12,
		RevenueTotal:   1200,
		PaymentFreq:    "quarterly",
	}

	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/contracts", bytes.NewReader(b))
	w := httptest.NewRecorder()

	handler.CreateContract(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var out api.Contract
	_ = json.NewDecoder(w.Body).Decode(&out)

	rows, _ := suite.DB.DB.Query(`
		SELECT due_date, amount
		FROM cashflow_entries
		WHERE contract_id = $1
		ORDER BY due_date
	`, out.ID)
	defer rows.Close()

	var count int
	var last time.Time
	for rows.Next() {
		var due time.Time
		var amount float64
		rows.Scan(&due, &amount)

		if count > 0 {
			expected := last.AddDate(0, 3, 0)
			if !due.Equal(expected) {
				t.Fatalf("expected quarterly spacing, got %v after %v", due, last)
			}
		}

		last = due
		count++
	}

	// Jan 2025 → Oct 2025 = 4 rows (0,3,6,9)
	if count != 4 {
		t.Fatalf("expected 4 quarterly rows, got %d", count)
	}
}

func TestProjection_OneTime(t *testing.T) {
	suite := factory.NewSuiteFromTestDB(t, testDB)
	testhelpers.TruncateAll(t, suite.DB)

	handler := &api.Handler{DB: suite.DB.DB}

	client := suite.CreateClient()
	sp := suite.CreateSalesProcessForClient(client.ID)

	body := api.Contract{
		ClientID:       client.ID,
		SalesProcessID: &sp.ID,
		StartDate:      "2025-01-01",
		DurationMonths: 12,
		RevenueTotal:   1200,
		PaymentFreq:    "one-time",
	}

	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/contracts", bytes.NewReader(b))
	w := httptest.NewRecorder()

	handler.CreateContract(w, req)

	var out api.Contract
	_ = json.NewDecoder(w.Body).Decode(&out)

	var count int
	_ = suite.DB.DB.QueryRow(`
		SELECT COUNT(*) FROM cashflow_entries WHERE contract_id=$1
	`, out.ID).Scan(&count)

	if count != 1 {
		t.Fatalf("expected 1 one-time projection row, got %d", count)
	}
}

func TestProjection_Shortening(t *testing.T) {
	suite := factory.NewSuiteFromTestDB(t, testDB)
	testhelpers.TruncateAll(t, suite.DB)

	handler := &api.Handler{DB: suite.DB.DB}

	client := suite.CreateClient()
	sp := suite.CreateSalesProcessForClient(client.ID)
	contract := suite.CreateContract(client.ID, sp.ID)

	update := api.UpdateContractRequest{
		StartDate:      "2025-01-01",
		DurationMonths: 6,
		RevenueTotal:   600,
		PaymentFreq:    "monthly",
	}

	b, _ := json.Marshal(update)
	req := httptest.NewRequest(http.MethodPatch,
		fmt.Sprintf("/api/contracts/%d", contract.ID),
		bytes.NewReader(b),
	)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.Itoa(contract.ID))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	handler.UpdateContract(w, req)

	var count int
	_ = suite.DB.DB.QueryRow(`
		SELECT COUNT(*) FROM cashflow_entries WHERE contract_id=$1
	`, contract.ID).Scan(&count)

	// Jan → Jun = 6 rows
	if count != 6 {
		t.Fatalf("expected 6 rows after shortening, got %d", count)
	}
}

func TestProjection_Extension(t *testing.T) {
	suite := factory.NewSuiteFromTestDB(t, testDB)
	testhelpers.TruncateAll(t, suite.DB)

	handler := &api.Handler{DB: suite.DB.DB}

	client := suite.CreateClient()
	sp := suite.CreateSalesProcessForClient(client.ID)

	body := api.Contract{
		ClientID:       client.ID,
		SalesProcessID: &sp.ID,
		StartDate:      "2025-01-01",
		DurationMonths: 6,
		RevenueTotal:   600,
		PaymentFreq:    "monthly",
	}

	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/contracts", bytes.NewReader(b))
	w := httptest.NewRecorder()
	handler.CreateContract(w, req)

	var out api.Contract
	_ = json.NewDecoder(w.Body).Decode(&out)

	update := api.UpdateContractRequest{
		StartDate:      "2025-01-01",
		DurationMonths: 12,
		RevenueTotal:   1200,
		PaymentFreq:    "monthly",
	}

	b2, _ := json.Marshal(update)
	req2 := httptest.NewRequest(http.MethodPatch,
		fmt.Sprintf("/api/contracts/%d", out.ID),
		bytes.NewReader(b2),
	)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.Itoa(out.ID))
	req2 = req2.WithContext(context.WithValue(req2.Context(), chi.RouteCtxKey, rctx))

	w2 := httptest.NewRecorder()
	handler.UpdateContract(w2, req2)

	var count int
	_ = suite.DB.DB.QueryRow(`
		SELECT COUNT(*) FROM cashflow_entries WHERE contract_id=$1
	`, out.ID).Scan(&count)

	// Jan 2025 → Dec 2025 = 12
	if count != 12 {
		t.Fatalf("expected 12 rows after extension, got %d", count)
	}
}

func TestProjection_StartDate31st(t *testing.T) {
	suite := factory.NewSuiteFromTestDB(t, testDB)
	testhelpers.TruncateAll(t, suite.DB)

	handler := &api.Handler{DB: suite.DB.DB}

	client := suite.CreateClient()
	sp := suite.CreateSalesProcessForClient(client.ID)

	body := api.Contract{
		ClientID:       client.ID,
		SalesProcessID: &sp.ID,
		StartDate:      "2025-01-31",
		DurationMonths: 2,
		RevenueTotal:   300,
		PaymentFreq:    "monthly",
	}

	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/contracts", bytes.NewReader(b))
	w := httptest.NewRecorder()

	handler.CreateContract(w, req)

	var out api.Contract
	_ = json.NewDecoder(w.Body).Decode(&out)

	rows, _ := suite.DB.DB.Query(`
		SELECT due_date FROM cashflow_entries
		WHERE contract_id=$1
		ORDER BY due_date
	`, out.ID)
	defer rows.Close()

	var dates []time.Time
	for rows.Next() {
		var d time.Time
		rows.Scan(&d)
		dates = append(dates, d)
	}

	// Go normalizes Feb 31 → Feb 28/29
	if len(dates) < 2 {
		t.Fatalf("expected at least 2 rows")
	}

	if dates[1].Month() != time.February {
		t.Fatalf("expected February rollover, got %v", dates[1])
	}
}
