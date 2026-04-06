package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
)

// ── DebugActiveClients ────────────────────────────────────────────────────────

func TestDebugActiveClients_DBError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT").WillReturnError(errTest("db down"))

	h := &Handler{DB: db}
	req := httptest.NewRequest(http.MethodGet, "/api/debug/active-clients", nil)
	w := httptest.NewRecorder()
	h.DebugActiveClients(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDebugActiveClients_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	rows := sqlmock.NewRows([]string{"name", "email", "end_date"}).
		AddRow("Acme Corp", nil, nil).
		AddRow("Beta GmbH", "beta@example.com", "2026-12-31")
	mock.ExpectQuery("SELECT").WillReturnRows(rows)

	h := &Handler{DB: db}
	req := httptest.NewRequest(http.MethodGet, "/api/debug/active-clients", nil)
	w := httptest.NewRecorder()
	h.DebugActiveClients(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if count, _ := resp["count"].(float64); int(count) != 2 {
		t.Fatalf("expected count=2, got %v", resp["count"])
	}
}

// ── DebugExpiredButActive ─────────────────────────────────────────────────────

func TestDebugExpiredButActive_DBError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT").WillReturnError(errTest("db down"))

	h := &Handler{DB: db}
	req := httptest.NewRequest(http.MethodGet, "/api/debug/expired-but-active", nil)
	w := httptest.NewRecorder()
	h.DebugExpiredButActive(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDebugExpiredButActive_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	rows := sqlmock.NewRows([]string{"id", "name", "email", "latest_end_date"}).
		AddRow(7, "Ghost Corp", nil, "2025-06-30")
	mock.ExpectQuery("SELECT").WillReturnRows(rows)

	h := &Handler{DB: db}
	req := httptest.NewRequest(http.MethodGet, "/api/debug/expired-but-active", nil)
	w := httptest.NewRecorder()
	h.DebugExpiredButActive(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if count, _ := resp["count"].(float64); int(count) != 1 {
		t.Fatalf("expected count=1, got %v", resp["count"])
	}
}

// ── DebugNoContracts ──────────────────────────────────────────────────────────

func TestDebugNoContracts_DBError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT").WillReturnError(errTest("db down"))

	h := &Handler{DB: db}
	req := httptest.NewRequest(http.MethodGet, "/api/debug/no-contracts", nil)
	w := httptest.NewRecorder()
	h.DebugNoContracts(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDebugNoContracts_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	rows := sqlmock.NewRows([]string{"id", "name", "email", "status"}).
		AddRow(3, "New Lead", nil, "initial_call_scheduled").
		AddRow(4, "Another", "a@example.com", "inactive")
	mock.ExpectQuery("SELECT").WillReturnRows(rows)

	h := &Handler{DB: db}
	req := httptest.NewRequest(http.MethodGet, "/api/debug/no-contracts", nil)
	w := httptest.NewRecorder()
	h.DebugNoContracts(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if count, _ := resp["count"].(float64); int(count) != 2 {
		t.Fatalf("expected count=2, got %v", resp["count"])
	}
}

// ── CreateClient pq error paths ───────────────────────────────────────────────

func TestCreateClient_DuplicateEmail_Returns409(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("INSERT INTO clients").
		WillReturnError(&pq.Error{Code: "23505", Constraint: "unique_client_email"})

	h := &Handler{DB: db}
	body := bytes.NewReader([]byte(`{"name":"Alice","email":"alice@example.com","status":"active"}`))
	req := httptest.NewRequest(http.MethodPost, "/api/clients", body)
	w := httptest.NewRecorder()
	h.CreateClient(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "E-Mail") {
		t.Fatalf("expected duplicate email message, got %q", w.Body.String())
	}
}

func TestCreateClient_OtherUniqueConstraint_Returns409(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("INSERT INTO clients").
		WillReturnError(&pq.Error{Code: "23505", Constraint: "some_other_constraint"})

	h := &Handler{DB: db}
	body := bytes.NewReader([]byte(`{"name":"Alice","status":"active"}`))
	req := httptest.NewRequest(http.MethodPost, "/api/clients", body)
	w := httptest.NewRecorder()
	h.CreateClient(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

// ── validateClientCompletedAt ─────────────────────────────────────────────────

func TestValidateClientCompletedAt_Nil(t *testing.T) {
	h := &Handler{DB: nil} // DB not touched when completedAt is nil
	if err := h.validateClientCompletedAt(context.Background(), 1, nil); err != nil {
		t.Fatalf("expected nil error for nil completedAt, got %v", err)
	}
}

func TestValidateClientCompletedAt_ClientNotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT created_at FROM clients").
		WillReturnError(sql.ErrNoRows)

	h := &Handler{DB: db}
	d := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	gotErr := h.validateClientCompletedAt(context.Background(), 99, &d)
	if gotErr == nil || gotErr.Error() != "client not found" {
		t.Fatalf("expected 'client not found', got %v", gotErr)
	}
}

func TestValidateClientCompletedAt_DBError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT created_at FROM clients").
		WillReturnError(errTest("connection lost"))

	h := &Handler{DB: db}
	d := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	if err := h.validateClientCompletedAt(context.Background(), 1, &d); err == nil {
		t.Fatal("expected error on DB failure, got nil")
	}
}

// ── DeleteClient sqlmock paths ────────────────────────────────────────────────

func TestDeleteClient_BeginTxFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin().WillReturnError(errTest("tx unavailable"))

	h := &Handler{DB: db}
	req := httptest.NewRequest(http.MethodDelete, "/api/clients/1", nil)
	w := httptest.NewRecorder()
	h.DeleteClient(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestDeleteClient_UpdateLeadsFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE leads").WillReturnError(errTest("leads update failed"))
	mock.ExpectRollback()

	h := &Handler{DB: db}
	req := httptest.NewRequest(http.MethodDelete, "/api/clients/1", nil)
	w := httptest.NewRecorder()
	h.DeleteClient(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteClient_DeleteExecFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE leads").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("DELETE FROM clients").WillReturnError(errTest("delete failed"))
	mock.ExpectRollback()

	h := &Handler{DB: db}
	req := httptest.NewRequest(http.MethodDelete, "/api/clients/1", nil)
	w := httptest.NewRecorder()
	h.DeleteClient(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteClient_CommitFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE leads").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("DELETE FROM clients").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit().WillReturnError(errTest("commit failed"))

	h := &Handler{DB: db}
	req := httptest.NewRequest(http.MethodDelete, "/api/clients/1", nil)
	w := httptest.NewRecorder()
	h.DeleteClient(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

// ── UpdateClient: unchanged completed_at skips revalidation ──────────────────

func TestUpdateClient_UnchangedCompletedAtSkipsValidation(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	existingDate := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	// SELECT completed_at query
	mock.ExpectQuery("SELECT completed_at FROM clients").
		WillReturnRows(sqlmock.NewRows([]string{"completed_at"}).AddRow(existingDate))
	// UPDATE clients
	mock.ExpectExec("UPDATE clients").WillReturnResult(sqlmock.NewResult(1, 1))
	// UPDATE leads sync
	mock.ExpectExec("UPDATE leads").WillReturnResult(sqlmock.NewResult(0, 0))

	h := &Handler{DB: db}
	body := bytes.NewReader([]byte(`{"completed_at":"2026-01-15"}`))
	req := httptest.NewRequest(http.MethodPatch, "/api/clients/1", body)
	w := httptest.NewRecorder()
	h.UpdateClient(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

// ── ListClients: comments batch load ─────────────────────────────────────────

func TestListClients_LoadsCommentsForClients(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	clientRows := sqlmock.NewRows([]string{"id", "lead_id", "name", "email", "phone", "source", "source_stage_name", "status", "completed_at"}).
		AddRow(int64(1), nil, "Acme", "acme@example.com", "123", "web", "", "active", nil)
	mock.ExpectQuery("WITH client_status").WillReturnRows(clientRows)

	commentRows := sqlmock.NewRows([]string{"id", "entity_id", "author", "body", "metadata", "created_at", "updated_at"}).
		AddRow(10, int64(1), "Admin", "First comment", nil, time.Now(), time.Now())
	mock.ExpectQuery("SELECT id, entity_id").WillReturnRows(commentRows)

	h := &Handler{DB: db}
	req := httptest.NewRequest(http.MethodGet, "/api/clients", nil)
	w := httptest.NewRecorder()
	h.ListClients(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var out []map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 client, got %d", len(out))
	}
	comments, _ := out[0]["comments"].([]interface{})
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
}

// errTest is a simple sentinel error for use in tests.
type errTest string

func (e errTest) Error() string { return string(e) }
