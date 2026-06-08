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
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/api"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/store"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/api/testhelpers"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/api/testhelpers/factory"
)

func TestCashflowForecast_Success_Postgres(t *testing.T) {
	suite := factory.NewSuiteFromTestDB(t, testDB)
	handler := api.NewHandler(store.New(suite.DB.DB), nil, nil)

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

func TestUpdateCashflowEntryStatus_ReflectedInListAndDetail(t *testing.T) {
	suite := factory.NewSuiteFromTestDB(t, testDB)
	handler := api.NewHandler(store.New(suite.DB.DB), nil, nil)

	testhelpers.TruncateAll(t, suite.DB)

	client := suite.CreateClient()
	// Ensure client is active (explicit for test visibility)
	_, err := suite.DB.DB.Exec(`UPDATE clients SET status = 'active' WHERE id = $1`, client.ID)
	if err != nil {
		t.Fatalf("failed to set client status: %v", err)
	}
	proc := suite.CreateSalesProcessForClient(client.ID)
	contract := suite.CreateContract(client.ID, proc.ID)
	// Set contract end_date far in the future to guarantee visibility
	_, err = suite.DB.DB.Exec(`UPDATE contracts SET end_date = CURRENT_DATE + INTERVAL '365 days' WHERE id = $1`, contract.ID)
	if err != nil {
		t.Fatalf("failed to set contract end_date: %v", err)
	}

	now := time.Now().UTC()
	suite.CreatePaidCashflow(contract.ID, now, 1200)

	// Get the entry ID
	var entryID int
	err = suite.DB.DB.QueryRow(`SELECT id FROM cashflow_entries WHERE contract_id = $1 LIMIT 1`, contract.ID).Scan(&entryID)
	if err != nil {
		t.Fatalf("failed to get cashflow entry id: %v", err)
	}

	// PATCH status to overdue
	patchBody := map[string]string{"status": "overdue"}
	b, _ := json.Marshal(patchBody)
	req := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/api/cashflow/entries/%d/status", entryID), bytes.NewReader(b))
	// Set chi URLParam context so handler can extract 'id'
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.Itoa(entryID))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	handler.UpdateCashflowEntryStatus(w, req)
	if w.Code != http.StatusNoContent {
		body := w.Body.String()
		t.Fatalf("expected 204, got %d. PATCH body: %s", w.Code, body)
	}

	t.Logf("contract.ID: %d", contract.ID)

	// Check via list endpoint (add client_id filter to match join logic)
	listReq := httptest.NewRequest(http.MethodGet, "/api/cashflow/entries?contract_id="+strconv.Itoa(contract.ID)+"&client_id="+strconv.Itoa(client.ID), nil)
	listW := httptest.NewRecorder()
	handler.ListCashflowEntries(listW, listReq)
	if listW.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. Body: %s", listW.Code, listW.Body.String())
	}
	var listResp struct {
		Data []struct {
			ID     int    `json:"id"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.NewDecoder(listW.Body).Decode(&listResp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	found := false
	for _, e := range listResp.Data {
		if e.ID == entryID {
			found = true
			if e.Status != "overdue" {
				t.Fatalf("expected status 'overdue', got '%s'", e.Status)
			}
		}
	}
	if !found {
		t.Fatalf("entry not found in list endpoint")
	}

	// Check via contract detail endpoint
	contractReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/contracts/%d", contract.ID), nil)
	rctxContract := chi.NewRouteContext()
	rctxContract.URLParams.Add("id", strconv.Itoa(contract.ID))
	contractReq = contractReq.WithContext(context.WithValue(contractReq.Context(), chi.RouteCtxKey, rctxContract))
	contractW := httptest.NewRecorder()
	handler.GetContract(contractW, contractReq)
	if contractW.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", contractW.Code)
	}
	var contractResp api.ContractResponse
	if err := json.NewDecoder(contractW.Body).Decode(&contractResp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	found = false
	for _, e := range contractResp.Cashflow {
		if e.ID == entryID {
			found = true
			if e.Status != "overdue" {
				t.Fatalf("expected status 'overdue' in detail, got '%s'", e.Status)
			}
		}
	}
	if !found {
		t.Fatalf("entry not found in contract detail endpoint")
	}
}
