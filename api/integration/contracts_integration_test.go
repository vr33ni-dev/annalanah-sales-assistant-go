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
	suite := factory.NewSuite(t)
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

	if got.PaidAmountTotal != 500 {
		t.Fatalf("expected paid_amount_total=500, got %v", got.PaidAmountTotal)
	}

	if got.PaidMonths != 1 {
		t.Fatalf("expected paid_months=1, got %d", got.PaidMonths)
	}
}

func TestCreateContract_Integration(t *testing.T) {
	suite := factory.NewSuite(t)
	handler := &api.Handler{DB: suite.DB.DB}

	testhelpers.TruncateAll(t, suite.DB)

	// Arrange: client + sales process
	client := suite.CreateClient()
	sp := suite.CreateSalesProcessForClient(client.ID)

	body := api.Contract{
		ClientID:       client.ID,
		SalesProcessID: sp.ID,
		StartDate:      "2025-01-01",
		DurationMonths: 12,
		RevenueTotal:   1200,
		PaymentFreq:    "monthly",
	}

	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/contracts", bytes.NewReader(b))
	w := httptest.NewRecorder()

	// Act
	handler.CreateContract(w, req)

	// Assert HTTP
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var out api.Contract
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if out.ID == 0 {
		t.Fatalf("expected generated contract ID")
	}

	if out.ClientID != client.ID {
		t.Fatalf("expected client_id=%d, got %d", client.ID, out.ClientID)
	}
}

func TestUpdateContract_Integration(t *testing.T) {
	suite := factory.NewSuite(t)

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
	suite := factory.NewSuite(t)

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
