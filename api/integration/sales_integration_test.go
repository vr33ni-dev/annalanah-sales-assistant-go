package integration_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vr33ni-dev/annalanah-sales-assistant-go/api"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/api/testhelpers"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/api/testhelpers/factory"
)

func strPtr(s string) *string { return &s }

// ----------------------------------------------------
// New client → allowed
// ----------------------------------------------------
func TestStartSalesProcess_NewClient(t *testing.T) {
	suite := factory.NewSuite(t)
	handler := &api.Handler{DB: suite.DB.DB}

	testhelpers.TruncateAll(t, suite.DB)

	body := api.StartSalesProcessRequest{
		Name:               "Bob",
		Email:              "b@example.com",
		Phone:              "999",
		Source:             "organic",
		InitialContactDate: strPtr("2025-10-01"),
		FollowUpDate:       strPtr("2025-11-01"),
	}

	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/sales/start", bytes.NewReader(b))
	w := httptest.NewRecorder()

	handler.StartSalesProcess(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

// ----------------------------------------------------
// Existing client + ACTIVE contract → BLOCKED
// ----------------------------------------------------
func TestStartSalesProcess_BlockedWhenActiveContractExists(t *testing.T) {
	suite := factory.NewSuite(t)
	handler := &api.Handler{DB: suite.DB.DB}

	testhelpers.TruncateAll(t, suite.DB)

	// seed client (let DB assign id)
	var existingClientID int
	testhelpers.MustQueryRow(t, suite.DB.DB, `
		INSERT INTO clients (name, email, phone, source)
		VALUES ('Bob', 'b@example.com', '999', 'organic') RETURNING id
	`).Scan(&existingClientID)

	// seed sales_process
	testhelpers.MustExec(t, suite.DB.DB, `
		INSERT INTO sales_process (id, client_id, stage)
		VALUES (100, $1, 'follow_up')
	`, existingClientID)

	// seed active contract linked to sales_process
	testhelpers.MustExec(t, suite.DB.DB, `
		INSERT INTO contracts (
			client_id,
			sales_process_id,
			start_date,
			duration_months,
			revenue_total,
			payment_frequency
		)
		VALUES ($1, 100, CURRENT_DATE, 12, 1200, 'monthly')
	`, existingClientID)

	body := api.StartSalesProcessRequest{
		Name:               "Bob Changed",
		Email:              "b@example.com",
		Phone:              "111",
		Source:             "paid",
		InitialContactDate: strPtr("2025-11-01"),
		FollowUpDate:       strPtr("2025-12-01"),
		ClientID:           &existingClientID,
	}

	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/sales/start", bytes.NewReader(b))
	w := httptest.NewRecorder()

	handler.StartSalesProcess(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

// ----------------------------------------------------
// Overwrite → updates BOTH client + lead
// ----------------------------------------------------
func TestStartSalesProcess_OverwriteAlsoUpdatesLead(t *testing.T) {
	suite := factory.NewSuite(t)
	handler := &api.Handler{DB: suite.DB.DB}

	testhelpers.TruncateAll(t, suite.DB)

	var existingClientID2 int
	testhelpers.MustQueryRow(t, suite.DB.DB, `
		INSERT INTO clients (name, email, phone, source)
		VALUES ('Dana', 'd@example.com', '555', 'organic') RETURNING id
	`).Scan(&existingClientID2)

	testhelpers.MustExec(t, suite.DB.DB, `
		INSERT INTO leads (name, email, phone, source, converted)
		VALUES ('Dana OLD', 'd@example.com', '555', 'organic', FALSE)
	`)

	body := api.StartSalesProcessRequest{
		Name:               "Dana Updated",
		Email:              "d@example.com",
		Phone:              "777",
		Source:             "paid",
		InitialContactDate: strPtr("2025-11-01"),
		FollowUpDate:       strPtr("2025-12-01"),
		MergeStrategy:      strPtr("overwrite"),
		ClientID:           &existingClientID2,
	}

	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/sales/start", bytes.NewReader(b))
	w := httptest.NewRecorder()

	handler.StartSalesProcess(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var clientName string
	testhelpers.MustQueryRow(t, suite.DB.DB,
		`SELECT name FROM clients WHERE email='d@example.com'`,
	).Scan(&clientName)

	if clientName != "Dana Updated" {
		t.Fatalf("client not overwritten, got %s", clientName)
	}

	var leadName string
	testhelpers.MustQueryRow(t, suite.DB.DB,
		`SELECT name FROM leads WHERE email='d@example.com' AND converted=FALSE`,
	).Scan(&leadName)

	if leadName != "Dana Updated" {
		t.Fatalf("lead not overwritten, got %s", leadName)
	}
}
