package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/api"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/api/testhelpers"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/api/testhelpers/factory"
)

// TestUpsellVerlaengerung_SetsClientStatusActive verifies that when an upsell
// with result "verlaengerung" is submitted, the client's status is flipped to
// 'active' as part of the same transaction.
func TestUpsellVerlaengerung_SetsClientStatusActive(t *testing.T) {
	suite := factory.NewSuiteFromTestDB(t, testDB)
	handler := &api.Handler{DB: suite.DB.DB}
	testhelpers.TruncateAll(t, suite.DB)

	client := suite.CreateClient() // status: 'inactive'
	sp := suite.CreateSalesProcessForClient(client.ID)

	// Create a previous contract that has already ended so the new one can start after it.
	suite.CreateContract(client.ID, sp.ID,
		factory.WithStartDate("2025-01-01"),
		factory.WithDuration(6),
	)

	body := map[string]any{
		"upsell_date":              "2026-04-06",
		"upsell_result":            "verlaengerung",
		"upsell_revenue":           1200.0,
		"contract_start_date":      "2026-08-01",
		"contract_duration_months": 6,
		"contract_frequency":       "monthly",
	}
	b, _ := json.Marshal(body)

	r := chi.NewRouter()
	r.Patch("/api/sales/{id}/upsell", handler.CreateOrUpdateUpsell)

	req := httptest.NewRequest(http.MethodPatch,
		fmt.Sprintf("/api/sales/%d/upsell", sp.ID),
		bytes.NewReader(b),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var status string
	testhelpers.MustQueryRow(t, suite.DB.DB,
		`SELECT status FROM clients WHERE id = $1`, client.ID,
	).Scan(&status)

	if status != "active" {
		t.Fatalf("expected client status 'active' after verlaengerung upsell, got %q", status)
	}
}
