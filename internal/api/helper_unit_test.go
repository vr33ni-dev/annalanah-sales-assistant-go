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

	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/domain"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/store"
)

func newTestHandler(t *testing.T) (*Handler, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	h := &Handler{store: store.New(db)}
	return h, mock, func() { db.Close() }
}

func TestGetNumericSetting_ReturnsValue(t *testing.T) {
	h, mock, cleanup := newTestHandler(t)
	defer cleanup()

	mock.ExpectQuery(`SELECT value_numeric FROM app_settings WHERE key = \$1`).
		WithArgs("timeout").
		WillReturnRows(sqlmock.NewRows([]string{"value_numeric"}).AddRow(2.5))

	got := h.getNumericSetting("timeout", 1.0)
	if got != 2.5 {
		t.Fatalf("expected 2.5, got %v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestGetNumericSetting_DefaultOnNullOrMissing(t *testing.T) {
	h, mock, cleanup := newTestHandler(t)
	defer cleanup()

	mock.ExpectQuery(`SELECT value_numeric FROM app_settings WHERE key = \$1`).
		WithArgs("missing").
		WillReturnRows(sqlmock.NewRows([]string{"value_numeric"}).AddRow(nil))

	got := h.getNumericSetting("missing", 7.2)
	if got != 7.2 {
		t.Fatalf("expected default 7.2, got %v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestGetTextSetting_ReturnsValue(t *testing.T) {
	h, mock, cleanup := newTestHandler(t)
	defer cleanup()

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
	h, mock, cleanup := newTestHandler(t)
	defer cleanup()

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
	pe := &pq.Error{Code: "23505", Constraint: "unique_client_email"}
	if !isUniqueViolation(pe, "unique_client_email") {
		t.Fatalf("expected true for matching pq error")
	}
	if isUniqueViolation(pe, "other") {
		t.Fatalf("expected false for non-matching constraint")
	}
	if isUniqueViolation(errors.New("boom"), "unique_client_email") {
		t.Fatalf("expected false for non-pq error")
	}
}

func TestUpsertSetting_ValidPotentialMonths(t *testing.T) {
	h, mock, cleanup := newTestHandler(t)
	defer cleanup()

	mock.ExpectExec(`INSERT INTO app_settings \(key, value_numeric, value_text, updated_at\)`).
		WithArgs("potential_months", sqlmock.AnyArg(), nil).
		WillReturnResult(sqlmock.NewResult(1, 1))

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
	var resp domain.AppSetting
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
	h, _, cleanup := newTestHandler(t)
	defer cleanup()

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
	h, mock, cleanup := newTestHandler(t)
	defer cleanup()

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
	var resp domain.AppSetting
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
	h, mock, cleanup := newTestHandler(t)
	defer cleanup()

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
	var resp domain.AppSetting
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

func TestGetSetting_NormalizesUpdatedAt(t *testing.T) {
	h, mock, cleanup := newTestHandler(t)
	defer cleanup()

	mock.ExpectQuery(`SELECT value_numeric,\s+value_text,\s+CAST\(updated_at AS text\)\s+FROM app_settings\s+WHERE key = \$1`).
		WithArgs("avg_revenue_per_contract").
		WillReturnRows(sqlmock.NewRows([]string{"value_numeric", "value_text", "updated_at"}).AddRow(600.0, nil, "2026-03-03 23:30:44"))

	req := httptest.NewRequest(http.MethodGet, "/api/settings/avg_revenue_per_contract", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("key", "avg_revenue_per_contract")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	h.GetSetting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}
	var resp domain.AppSetting
	if err := json.NewDecoder(bytes.NewReader(w.Body.Bytes())).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.UpdatedAt == nil || *resp.UpdatedAt != "2026-03-03T23:30:44Z" {
		t.Fatalf("expected normalized updated_at, got %v", resp.UpdatedAt)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestListSettings_NormalizesUpdatedAt(t *testing.T) {
	h, mock, cleanup := newTestHandler(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"key", "value_numeric", "value_text", "updated_at"}).
		AddRow("avg_revenue_per_contract", 600.0, nil, "2026-03-03 23:30:44").
		AddRow("new_contract_notify_email", nil, "ops@example.com", "weird-but-keep")
	mock.ExpectQuery(`SELECT key,\s+value_numeric,\s+value_text,\s+CAST\(updated_at AS text\)\s+FROM app_settings\s+ORDER BY key`).
		WillReturnRows(rows)

	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	w := httptest.NewRecorder()

	h.ListSettings(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}
	var resp []domain.AppSetting
	if err := json.NewDecoder(bytes.NewReader(w.Body.Bytes())).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp) != 2 {
		t.Fatalf("expected 2 settings, got %d", len(resp))
	}
	if resp[0].UpdatedAt == nil || *resp[0].UpdatedAt != "2026-03-03T23:30:44Z" {
		t.Fatalf("expected first updated_at normalized, got %v", resp[0].UpdatedAt)
	}
	if resp[1].UpdatedAt == nil || *resp[1].UpdatedAt != "weird-but-keep" {
		t.Fatalf("expected second updated_at passthrough, got %v", resp[1].UpdatedAt)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func strPtr(s string) *string { return &s }

func TestUpsertSetting_MissingBothValues(t *testing.T) {
	h, _, cleanup := newTestHandler(t)
	defer cleanup()

	body := map[string]interface{}{}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, "/api/settings/some_key", bytes.NewReader(b))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("key", "some_key")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	h.UpsertSetting(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpsertSetting_BadJSON(t *testing.T) {
	h, _, cleanup := newTestHandler(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPut, "/api/settings/some_key", bytes.NewBufferString("{bad json}"))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("key", "some_key")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	h.UpsertSetting(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpsertSetting_DBExecError(t *testing.T) {
	h, mock, cleanup := newTestHandler(t)
	defer cleanup()

	mock.ExpectExec(`INSERT INTO app_settings`).
		WillReturnError(errors.New("db error"))

	v := 3.0
	body := domain.AppSetting{ValueNumeric: &v}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, "/api/settings/some_setting", bytes.NewReader(b))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("key", "some_setting")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	h.UpsertSetting(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpsertSetting_TextValue_Success(t *testing.T) {
	h, mock, cleanup := newTestHandler(t)
	defer cleanup()

	mock.ExpectExec(`INSERT INTO app_settings`).
		WithArgs("notify_email", nil, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectQuery(`SELECT value_numeric,\s+value_text,\s+CAST\(updated_at AS text\)\s+FROM app_settings\s+WHERE key = \$1`).
		WithArgs("notify_email").
		WillReturnRows(sqlmock.NewRows([]string{"value_numeric", "value_text", "updated_at"}).AddRow(nil, "test@example.com", nil))

	txt := "test@example.com"
	body := domain.AppSetting{ValueText: &txt}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, "/api/settings/notify_email", bytes.NewReader(b))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("key", "notify_email")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	h.UpsertSetting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestUpsertSetting_PotentialMonths_RequiresNumeric(t *testing.T) {
	h, _, cleanup := newTestHandler(t)
	defer cleanup()

	txt := "six"
	body := domain.AppSetting{ValueText: &txt}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, "/api/settings/potential_months", bytes.NewReader(b))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("key", "potential_months")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	h.UpsertSetting(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListSettings_DBQueryError_Returns500(t *testing.T) {
	h, mock, cleanup := newTestHandler(t)
	defer cleanup()

	mock.ExpectQuery("SELECT key").WillReturnError(errors.New("db unavailable"))

	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	w := httptest.NewRecorder()
	h.ListSettings(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListSettings_ScanError_Returns500(t *testing.T) {
	h, mock, cleanup := newTestHandler(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"key"}).AddRow("some_key")
	mock.ExpectQuery("SELECT key").WillReturnRows(rows)

	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	w := httptest.NewRecorder()
	h.ListSettings(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetSetting_OtherKey_NotFound_Returns404(t *testing.T) {
	h, mock, cleanup := newTestHandler(t)
	defer cleanup()

	mock.ExpectQuery("SELECT value_numeric").
		WithArgs("mwst_rate").
		WillReturnError(sql.ErrNoRows)

	req := httptest.NewRequest(http.MethodGet, "/api/settings/mwst_rate", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("key", "mwst_rate")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	h.GetSetting(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetSetting_DBQueryError_Returns500(t *testing.T) {
	h, mock, cleanup := newTestHandler(t)
	defer cleanup()

	mock.ExpectQuery("SELECT value_numeric").
		WithArgs("some_key").
		WillReturnError(errors.New("db down"))

	req := httptest.NewRequest(http.MethodGet, "/api/settings/some_key", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("key", "some_key")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	h.GetSetting(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}
