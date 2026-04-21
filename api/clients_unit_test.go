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

	commentRows := sqlmock.NewRows([]string{"id", "client_id", "author", "body", "metadata", "created_at", "updated_at"}).
		AddRow(10, int64(1), "Admin", "First comment", nil, time.Now(), time.Now())
	mock.ExpectQuery("SELECT id, client_id").WillReturnRows(commentRows)

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

// ── validateClientCompletedAt: follow_up query DB error ───────────────────────

func TestValidateClientCompletedAt_FollowUpQueryError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	createdAt := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery("SELECT created_at FROM clients").
		WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(createdAt))
	mock.ExpectQuery("SELECT follow_up_date").
		WillReturnError(errTest("follow_up query failed"))

	h := &Handler{DB: db}
	d := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	if err := h.validateClientCompletedAt(context.Background(), 1, &d); err == nil {
		t.Fatal("expected error on follow_up query failure, got nil")
	}
}

// ── UpdateClient: duplicate email on update ───────────────────────────────────

func TestUpdateClient_DuplicateEmailOnUpdate(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	// SELECT completed_at returns null (no existing date)
	mock.ExpectQuery("SELECT completed_at FROM clients").
		WillReturnRows(sqlmock.NewRows([]string{"completed_at"}).AddRow(nil))
	// UPDATE clients returns pq 23505
	mock.ExpectExec("UPDATE clients").
		WillReturnError(&pq.Error{Code: "23505", Constraint: "unique_client_email"})

	h := &Handler{DB: db}
	body := bytes.NewReader([]byte(`{"email":"duplicate@example.com"}`))
	req := httptest.NewRequest(http.MethodPatch, "/api/clients/1", body)
	w := httptest.NewRecorder()
	h.UpdateClient(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "E-Mail") {
		t.Fatalf("expected duplicate email message, got %q", w.Body.String())
	}
}

// ── UpdateClient: lead sync failure (non-fatal) ───────────────────────────────

func TestUpdateClient_LeadSyncFailsNonFatal(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT completed_at FROM clients").
		WillReturnRows(sqlmock.NewRows([]string{"completed_at"}).AddRow(nil))
	mock.ExpectExec("UPDATE clients").WillReturnResult(sqlmock.NewResult(1, 1))
	// Lead sync fails (non-fatal)
	mock.ExpectExec("UPDATE leads").WillReturnError(errTest("lead sync down"))

	h := &Handler{DB: db}
	body := bytes.NewReader([]byte(`{"name":"New Name"}`))
	req := httptest.NewRequest(http.MethodPatch, "/api/clients/1", body)
	w := httptest.NewRecorder()
	h.UpdateClient(w, req)

	// Still 204 — lead sync failure is non-fatal
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

// ── UpdateClient: comments in PATCH ──────────────────────────────────────────

func TestUpdateClient_WithComments(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT completed_at FROM clients").
		WillReturnRows(sqlmock.NewRows([]string{"completed_at"}).AddRow(nil))
	mock.ExpectExec("UPDATE clients").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("UPDATE leads").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO comments").WillReturnResult(sqlmock.NewResult(1, 1))

	h := &Handler{DB: db}
	body := bytes.NewReader([]byte(`{"name":"Bob","comments":[{"body":"a note"}]}`))
	req := httptest.NewRequest(http.MethodPatch, "/api/clients/1", body)
	w := httptest.NewRecorder()
	h.UpdateClient(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

// ── ListClients: comment scan error ──────────────────────────────────────────

func TestListClients_CommentScanError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	clientRows := sqlmock.NewRows([]string{"id", "lead_id", "name", "email", "phone", "source", "source_stage_name", "status", "completed_at"}).
		AddRow(int64(1), nil, "Acme", "acme@example.com", "123", "web", "", "active", nil)
	mock.ExpectQuery("WITH client_status").WillReturnRows(clientRows)

	// Return rows with only 1 column — scan of 7 columns fails → continue
	commentRows := sqlmock.NewRows([]string{"id"}).AddRow(99)
	mock.ExpectQuery("SELECT id, client_id").WillReturnRows(commentRows)

	h := &Handler{DB: db}
	req := httptest.NewRequest(http.MethodGet, "/api/clients", nil)
	w := httptest.NewRecorder()
	h.ListClients(w, req)

	// Scan error is silently continued — handler still returns 200
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// ── ListClients: comment metadata JSON unmarshalling ─────────────────────────

func TestListClients_CommentWithMetadata(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	clientRows := sqlmock.NewRows([]string{"id", "lead_id", "name", "email", "phone", "source", "source_stage_name", "status", "completed_at"}).
		AddRow(int64(1), nil, "Acme", "acme@example.com", "123", "web", "", "active", nil)
	mock.ExpectQuery("WITH client_status").WillReturnRows(clientRows)

	metaJSON := `{"key":"value"}`
	commentRows := sqlmock.NewRows([]string{"id", "client_id", "author", "body", "metadata", "created_at", "updated_at"}).
		AddRow(5, int64(1), nil, "body text", metaJSON, time.Now(), time.Now())
	mock.ExpectQuery("SELECT id, client_id").WillReturnRows(commentRows)

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
	comments, _ := out[0]["comments"].([]interface{})
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	c := comments[0].(map[string]interface{})
	if meta, _ := c["metadata"].(map[string]interface{}); meta["key"] != "value" {
		t.Fatalf("expected metadata key=value, got %v", c["metadata"])
	}
}

// ── ListClients: main rows.Scan error returns 500 ────────────────────────────

func TestListClients_MainScanError_Returns500(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	// Return only 1 column instead of the expected 9 → forces Scan to fail
	rows := sqlmock.NewRows([]string{"id"}).AddRow(42)
	mock.ExpectQuery("WITH client_status").WillReturnRows(rows)

	h := &Handler{DB: db}
	req := httptest.NewRequest(http.MethodGet, "/api/clients", nil)
	w := httptest.NewRecorder()
	h.ListClients(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

// ── ListClients: non-nil leadID sets LeadID field ────────────────────────────

func TestListClients_WithLeadID(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	leadID := int64(7)
	clientRows := sqlmock.NewRows([]string{"id", "lead_id", "name", "email", "phone", "source", "source_stage_name", "status", "completed_at"}).
		AddRow(int64(1), leadID, "Acme", "acme@example.com", "123", "web", "", "active", nil)
	mock.ExpectQuery("WITH client_status").WillReturnRows(clientRows)

	// No comments
	mock.ExpectQuery("SELECT id, client_id").WillReturnRows(sqlmock.NewRows([]string{"id", "client_id", "author", "body", "metadata", "created_at", "updated_at"}))

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
	gotLeadID, _ := out[0]["lead_id"].(float64)
	if gotLeadID != float64(leadID) {
		t.Fatalf("expected lead_id=%d, got %v", leadID, out[0]["lead_id"])
	}
}

// ── CreateClient: non-pq DB error returns 500 ────────────────────────────────

func TestCreateClient_NonPQError_Returns500(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("INSERT INTO clients").
		WillReturnError(errTest("connection refused"))

	h := &Handler{DB: db}
	body := bytes.NewReader([]byte(`{"name":"Alice","status":"active"}`))
	req := httptest.NewRequest(http.MethodPost, "/api/clients", body)
	w := httptest.NewRecorder()
	h.CreateClient(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

// ── UpdateClient: invalid completed_at format returns 400 ────────────────────

func TestUpdateClient_InvalidCompletedAt_Returns400(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	// The date parse failure is caught before any DB call, so no expectations needed.
	h := &Handler{DB: db}
	body := bytes.NewReader([]byte(`{"completed_at":"not-a-date"}`))
	req := httptest.NewRequest(http.MethodPatch, "/api/clients/1", body)
	w := httptest.NewRecorder()
	h.UpdateClient(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "invalid completed_at") {
		t.Fatalf("expected 'invalid completed_at' message, got %q", w.Body.String())
	}
}

// ── UpdateClient: second JSON unmarshal failure (invalid field type) → 400 ───

func TestUpdateClient_SecondUnmarshalError_Returns400(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	// First unmarshal into map succeeds, second into typed struct fails
	// because source_stage_id must be *int but receives a string.
	h := &Handler{DB: db}
	body := bytes.NewReader([]byte(`{"source_stage_id":"not-a-number"}`))
	req := httptest.NewRequest(http.MethodPatch, "/api/clients/1", body)
	w := httptest.NewRecorder()
	h.UpdateClient(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// ── UpdateClient: comment insert failure is non-fatal ────────────────────────

func TestUpdateClient_CommentInsertFailsNonFatal(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT completed_at FROM clients").
		WillReturnRows(sqlmock.NewRows([]string{"completed_at"}).AddRow(nil))
	mock.ExpectExec("UPDATE clients").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("UPDATE leads").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO comments").WillReturnError(errTest("comment insert failed"))

	h := &Handler{DB: db}
	body := bytes.NewReader([]byte(`{"name":"Bob","comments":[{"body":"a note"}]}`))
	req := httptest.NewRequest(http.MethodPatch, "/api/clients/1", body)
	w := httptest.NewRecorder()
	h.UpdateClient(w, req)

	// Comment failure is non-fatal — still returns 204
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

// ── CreateClient: comment insert failure is non-fatal ────────────────────────

func TestCreateClient_CommentInsertFailsNonFatal(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("INSERT INTO clients").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(42))
	mock.ExpectExec("INSERT INTO comments").
		WillReturnError(errTest("comment insert failed"))

	h := &Handler{DB: db}
	body := bytes.NewReader([]byte(`{"name":"Alice","status":"active","comments":[{"body":"first note"}]}`))
	req := httptest.NewRequest(http.MethodPost, "/api/clients", body)
	w := httptest.NewRecorder()
	h.CreateClient(w, req)

	// Comment failure is non-fatal — still returns 201
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}
