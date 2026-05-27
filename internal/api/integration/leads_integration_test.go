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

	"github.com/go-chi/chi/v5"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/api"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/store"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/api/testhelpers"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/api/testhelpers/factory"
)

func TestCreateLead_Integration_Success(t *testing.T) {
	suite := factory.NewSuiteFromTestDB(t, testDB)

	testhelpers.TruncateAll(t, suite.DB)
	handler := api.NewHandler(store.New(suite.DB.DB), nil, nil)

	body := map[string]interface{}{
		"name":   "Alice",
		"email":  "a@test.com",
		"phone":  "123",
		"source": "organic",
	}

	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/leads", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.CreateLead(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestCreateLead_Integration_DuplicateEmail(t *testing.T) {
	suite := factory.NewSuiteFromTestDB(t, testDB)

	testhelpers.TruncateAll(t, suite.DB)
	handler := api.NewHandler(store.New(suite.DB.DB), nil, nil)

	// seed lead
	testhelpers.MustExec(t, suite.DB.DB, `
		INSERT INTO leads (name, email, source)
		VALUES ('Alice', 'a@test.com', 'organic')
	`)

	body := map[string]string{
		"name":   "Alice 2",
		"email":  "a@test.com",
		"source": "organic",
	}

	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/leads", bytes.NewReader(b))
	w := httptest.NewRecorder()

	handler.CreateLead(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestUpdateLead_Integration(t *testing.T) {
	suite := factory.NewSuiteFromTestDB(t, testDB)

	testhelpers.TruncateAll(t, suite.DB)
	handler := api.NewHandler(store.New(suite.DB.DB), nil, nil)

	var id int
	testhelpers.MustQueryRow(t, suite.DB.DB,
		`INSERT INTO leads (name, source) VALUES ('Old', 'organic') RETURNING id`,
	).Scan(&id)

	body := map[string]string{"name": "New"}
	b, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/api/leads/%d", id), bytes.NewReader(b))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.Itoa(id))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	handler.UpdateLead(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var name string
	testhelpers.MustQueryRow(t, suite.DB.DB,
		`SELECT name FROM leads WHERE id=$1`, id,
	).Scan(&name)

	if name != "New" {
		t.Fatalf("expected updated name")
	}
}

func TestDeleteLead_Integration(t *testing.T) {
	suite := factory.NewSuiteFromTestDB(t, testDB)

	testhelpers.TruncateAll(t, suite.DB)
	handler := api.NewHandler(store.New(suite.DB.DB), nil, nil)

	var id int
	testhelpers.MustQueryRow(t, suite.DB.DB,
		`INSERT INTO leads (name, source) VALUES ('X', 'organic') RETURNING id`,
	).Scan(&id)

	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/leads/%d", id), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.Itoa(id))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	handler.DeleteLead(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

// successful conversion (new client + sales process)
func TestConvertLead_Success(t *testing.T) {
	suite := factory.NewSuiteFromTestDB(t, testDB)

	testhelpers.TruncateAll(t, suite.DB)
	handler := api.NewHandler(store.New(suite.DB.DB), nil, nil)

	var leadID int
	testhelpers.MustQueryRow(t, suite.DB.DB, `
		INSERT INTO leads (name, email, source)
		VALUES ('Alice', 'a@test.com', 'organic')
		RETURNING id
	`).Scan(&leadID)

	req := httptest.NewRequest(
		http.MethodPost,
		fmt.Sprintf("/api/leads/%d/convert", leadID),
		nil,
	)

	r := chi.NewRouter()
	r.Post("/api/leads/{id}/convert", handler.ConvertLead)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}

	var resp map[string]int
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if resp["client_id"] == 0 || resp["sales_process_id"] == 0 {
		t.Fatalf("expected client_id and sales_process_id, got %+v", resp)
	}

	var clientStatus string
	testhelpers.MustQueryRow(
		t,
		suite.DB.DB,
		`SELECT status FROM clients WHERE id = $1`,
		resp["client_id"],
	).Scan(&clientStatus)

	if clientStatus != "follow_up_scheduled" {
		t.Fatalf("expected client status follow_up_scheduled, got %q", clientStatus)
	}

	var converted bool
	testhelpers.MustQueryRow(
		t,
		suite.DB.DB,
		`SELECT converted FROM leads WHERE id=$1`,
		leadID,
	).Scan(&converted)

	if !converted {
		t.Fatalf("expected lead to be marked converted")
	}
}

// idempotent conversion (already converted)
func TestConvertLead_Idempotent(t *testing.T) {
	suite := factory.NewSuiteFromTestDB(t, testDB)

	testhelpers.TruncateAll(t, suite.DB)
	handler := api.NewHandler(store.New(suite.DB.DB), nil, nil)

	var leadID int
	testhelpers.MustQueryRow(t, suite.DB.DB, `
		INSERT INTO leads (name, email, source)
		VALUES ('Bob', 'b@test.com', 'paid')
		RETURNING id
	`).Scan(&leadID)

	// First conversion
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/leads/%d/convert", leadID), nil)

	r := chi.NewRouter()
	r.Post("/api/leads/{id}/convert", handler.ConvertLead)

	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req)

	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req)

	if w2.Code != http.StatusOK {
		t.Fatalf("expected idempotent 200, got %d", w2.Code)
	}
}
