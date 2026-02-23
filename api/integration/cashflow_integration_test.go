package integration_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/vr33ni-dev/annalanah-sales-assistant-go/api"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/api/testhelpers"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/api/testhelpers/factory"
)

func TestCashflowForecast_Success_Postgres(t *testing.T) {
	suite := factory.NewSuiteFromTestDB(t, testDB)
	handler := &api.Handler{DB: suite.DB.DB}

	testhelpers.TruncateAll(t, suite.DB)

	client := suite.CreateClient()
	proc := suite.CreateSalesProcessForClient(client.ID)
	contract := suite.CreateContract(client.ID, proc.ID)

	now := time.Now().UTC()
	now2 := now.AddDate(0, 1, 0)
	suite.CreatePaidCashflow(contract.ID, now, 1200)
	suite.CreatePaidCashflow(contract.ID, now2, 900)

	req := httptest.NewRequest(http.MethodGet, "/api/cashflow", nil)
	w := httptest.NewRecorder()

	handler.CashflowForecast(w, req)

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
		t.Fatalf("expected 6 rows, got %d", len(rows))
	}

	// both paid entries should sum to 2100 across the forecast window
	var totalConfirmed float64
	for _, r := range rows {
		totalConfirmed += r.Confirmed
	}
	if totalConfirmed != 2100 {
		t.Fatalf("expected total confirmed=2100, got=%v", totalConfirmed)
	}
}
