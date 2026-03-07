package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-chi/chi/v5"
	"github.com/lib/pq"
)

func TestGetNumericSetting_ReturnsValue(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to open sqlmock: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}

	mock.ExpectQuery(`SELECT value_numeric FROM app_settings WHERE key = \$1`).
		WithArgs("timeout").
		WillReturnRows(func() *sqlmock.Rows {
			r := sqlmock.NewRows([]string{"value_numeric"}).AddRow(2.5)
			return r
		}())

	got := h.getNumericSetting("timeout", 1.0)
	if got != 2.5 {
		t.Fatalf("expected 2.5, got %v", got)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestGetNumericSetting_DefaultOnNullOrMissing(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to open sqlmock: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}

	// return NULL
	mock.ExpectQuery(`SELECT value_numeric FROM app_settings WHERE key = \$1`).
		WithArgs("missing").
		WillReturnRows(func() *sqlmock.Rows {
			r := sqlmock.NewRows([]string{"value_numeric"}).AddRow(nil)
			return r
		}())

	got := h.getNumericSetting("missing", 7.2)
	if got != 7.2 {
		t.Fatalf("expected default 7.2, got %v", got)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestGetTextSetting_ReturnsValue(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to open sqlmock: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}

	mock.ExpectQuery(`SELECT value_text FROM app_settings WHERE key = \$1`).
		WithArgs("new_contract_notify_email").
		WillReturnRows(sqlmock.NewRows([]string{"value_text"}).AddRow("ops@example.com"))

	got := h.getTextSetting("new_contract_notify_email", "")
	if got != "ops@example.com" {
		t.Fatalf("expected ops@example.com, got %q", got)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestGetTextSetting_DefaultOnNullOrBlank(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to open sqlmock: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}

	mock.ExpectQuery(`SELECT value_text FROM app_settings WHERE key = \$1`).
		WithArgs("new_contract_notify_email").
		WillReturnRows(sqlmock.NewRows([]string{"value_text"}).AddRow(nil))

	got := h.getTextSetting("new_contract_notify_email", "fallback@example.com")
	if got != "fallback@example.com" {
		t.Fatalf("expected fallback@example.com, got %q", got)
	}

	mock.ExpectQuery(`SELECT value_text FROM app_settings WHERE key = \$1`).
		WithArgs("new_contract_notify_email").
		WillReturnRows(sqlmock.NewRows([]string{"value_text"}).AddRow("   "))

	got = h.getTextSetting("new_contract_notify_email", "fallback@example.com")
	if got != "fallback@example.com" {
		t.Fatalf("expected fallback@example.com for blank setting, got %q", got)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestIsUniqueViolation(t *testing.T) {
	// matching pq error
	pe := &pq.Error{Code: "23505", Constraint: "unique_client_email"}
	if !isUniqueViolation(pe, "unique_client_email") {
		t.Fatalf("expected true for matching pq error")
	}

	// wrong constraint
	if isUniqueViolation(pe, "other") {
		t.Fatalf("expected false for non-matching constraint")
	}

	// non-pq error
	if isUniqueViolation(errors.New("boom"), "unique_client_email") {
		t.Fatalf("expected false for non-pq error")
	}
}

func TestUpsertSetting_ValidPotentialMonths(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to open sqlmock: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}

	// Expect Exec for upsert
	mock.ExpectExec(`INSERT INTO app_settings \(key, value_numeric, value_text, updated_at\)`).
		WithArgs("potential_months", sqlmock.AnyArg(), nil).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Expect SELECT in GetSetting to return the stored value
	mock.ExpectQuery(`SELECT value_numeric,\s+value_text,\s+CAST\(updated_at AS text\)\s+FROM app_settings\s+WHERE key = \$1`).
		WithArgs("potential_months").
		WillReturnRows(sqlmock.NewRows([]string{"value_numeric", "value_text", "updated_at"}).AddRow(12.0, nil, nil))

	body := map[string]float64{"value_numeric": 12}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, "/api/settings/potential_months", bytes.NewReader(b))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("key", "potential_months")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	h.UpsertSetting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	// simple check that response contains the key and numeric value
	var resp AppSetting
	if err := json.NewDecoder(bytes.NewReader(w.Body.Bytes())).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Key != "potential_months" {
		t.Fatalf("expected key potential_months, got %s", resp.Key)
	}
	if resp.ValueNumeric == nil || *resp.ValueNumeric != 12 {
		t.Fatalf("expected numeric 12, got %v", resp.ValueNumeric)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestUpsertSetting_InvalidPotentialMonths(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to open sqlmock: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}

	cases := []map[string]float64{{"value_numeric": 0}, {"value_numeric": -1}, {"value_numeric": 2.5}}
	for _, c := range cases {
		b, _ := json.Marshal(c)
		req := httptest.NewRequest(http.MethodPut, "/api/settings/potential_months", bytes.NewReader(b))
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("key", "potential_months")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()
		h.UpsertSetting(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for input %v, got %d", c, w.Code)
		}
	}
}

func TestGetSetting_NewContractNotifyEmail_NoRow_UsesEnvFallback(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to open sqlmock: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}

	mock.ExpectQuery(`SELECT value_numeric,\s+value_text,\s+CAST\(updated_at AS text\)\s+FROM app_settings\s+WHERE key = \$1`).
		WithArgs("new_contract_notify_email").
		WillReturnError(sql.ErrNoRows)

	os.Setenv("NEW_CONTRACT_NOTIFY_EMAIL", "ops@example.com")
	defer os.Unsetenv("NEW_CONTRACT_NOTIFY_EMAIL")

	req := httptest.NewRequest(http.MethodGet, "/api/settings/new_contract_notify_email", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("key", "new_contract_notify_email")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	h.GetSetting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var resp AppSetting
	if err := json.NewDecoder(bytes.NewReader(w.Body.Bytes())).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Key != "new_contract_notify_email" {
		t.Fatalf("expected key new_contract_notify_email, got %s", resp.Key)
	}
	if resp.ValueText == nil || *resp.ValueText != "ops@example.com" {
		t.Fatalf("expected value_text ops@example.com, got %v", resp.ValueText)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestGetSetting_NewContractNotifyEmail_NoRow_NoEnv_ReturnsEmptyPayload(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to open sqlmock: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}

	mock.ExpectQuery(`SELECT value_numeric,\s+value_text,\s+CAST\(updated_at AS text\)\s+FROM app_settings\s+WHERE key = \$1`).
		WithArgs("new_contract_notify_email").
		WillReturnError(sql.ErrNoRows)

	os.Unsetenv("NEW_CONTRACT_NOTIFY_EMAIL")

	req := httptest.NewRequest(http.MethodGet, "/api/settings/new_contract_notify_email", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("key", "new_contract_notify_email")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	h.GetSetting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var resp AppSetting
	if err := json.NewDecoder(bytes.NewReader(w.Body.Bytes())).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Key != "new_contract_notify_email" {
		t.Fatalf("expected key new_contract_notify_email, got %s", resp.Key)
	}
	if resp.ValueText != nil {
		t.Fatalf("expected empty value_text when env unset, got %v", *resp.ValueText)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
