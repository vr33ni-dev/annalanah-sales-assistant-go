package api_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/api"
)

func TestUpdateComment_BadJSON(t *testing.T) {
	h := &api.Handler{DB: nil}

	req := httptest.NewRequest(http.MethodPatch, "/api/comments/1", bytes.NewReader([]byte("{bad")))
	w := httptest.NewRecorder()

	h.UpdateComment(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpdateComment_NoFields(t *testing.T) {
	h := &api.Handler{DB: nil}

	req := httptest.NewRequest(http.MethodPatch, "/api/comments/1", bytes.NewReader([]byte(`{}`)))
	w := httptest.NewRecorder()

	h.UpdateComment(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpdateComment_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &api.Handler{DB: db}

	// Expect an update query (we match by prefix)
	rows := sqlmock.NewRows([]string{"id", "entity_type", "entity_id", "author", "body", "metadata", "created_at", "updated_at"}).
		AddRow(1, "client", 42, "Bob", "updated body", `{"m":"v"}`, time.Now(), time.Now())

	mock.ExpectQuery("UPDATE comments SET").WillReturnRows(rows)

	payload := map[string]interface{}{"body": "updated body", "metadata": map[string]string{"m": "v"}}
	b, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPatch, "/api/comments/1", bytes.NewReader(b))
	w := httptest.NewRecorder()

	h.UpdateComment(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp api.CommentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if resp.Body != "updated body" {
		t.Fatalf("unexpected body: %s", resp.Body)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestUpdateComment_NotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &api.Handler{DB: db}

	mock.ExpectQuery("UPDATE comments SET").WillReturnError(sql.ErrNoRows)

	payload := map[string]string{"body": "x"}
	b, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPatch, "/api/comments/99", bytes.NewReader(b))
	w := httptest.NewRecorder()

	h.UpdateComment(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestCreateComment_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &api.Handler{DB: db}

	payload := map[string]interface{}{
		"entity_type": "client",
		"entity_id":   42,
		"author":      "Alice",
		"body":        "hello world",
		"metadata":    map[string]string{"m": "v"},
	}
	b, _ := json.Marshal(payload)

	// Expect INSERT returning id, created_at, updated_at
	now := time.Now()
	rows := sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).AddRow(1, now, now)
	mock.ExpectQuery("INSERT INTO comments").WillReturnRows(rows)

	req := httptest.NewRequest(http.MethodPost, "/api/comments", bytes.NewReader(b))
	w := httptest.NewRecorder()

	h.CreateComment(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d; body=%s", w.Code, w.Body.String())
	}

	var resp api.CommentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if resp.ID != 1 {
		t.Fatalf("unexpected id: %d", resp.ID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestCreateComment_DBError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &api.Handler{DB: db}

	payload := map[string]interface{}{
		"entity_type": "client",
		"entity_id":   42,
		"author":      "Alice",
		"body":        "hello world",
	}
	b, _ := json.Marshal(payload)

	// Simulate DB error (e.g., missing table)
	mock.ExpectQuery("INSERT INTO comments").WillReturnError(sql.ErrConnDone)

	req := httptest.NewRequest(http.MethodPost, "/api/comments", bytes.NewReader(b))
	w := httptest.NewRecorder()

	h.CreateComment(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d; body=%s", w.Code, w.Body.String())
	}

	if !strings.Contains(w.Body.String(), "internal server error") && w.Body.String() == "" {
		// ensure some error text was returned; http.Error uses the err text
		t.Logf("response body: %q", w.Body.String())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestCreateComment_ContentAlias(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &api.Handler{DB: db}

	payload := map[string]interface{}{
		"entity_type": "client",
		"entity_id":   7,
		"author":      "Eve",
		"content":     "alias body",
	}
	b, _ := json.Marshal(payload)

	now := time.Now()
	rows := sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).AddRow(5, now, now)
	mock.ExpectQuery("INSERT INTO comments").WillReturnRows(rows)

	req := httptest.NewRequest(http.MethodPost, "/api/comments", bytes.NewReader(b))
	w := httptest.NewRecorder()

	h.CreateComment(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d; body=%s", w.Code, w.Body.String())
	}

	var resp api.CommentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if resp.Body != "alias body" {
		t.Fatalf("unexpected body: %s", resp.Body)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestCreateComment_MissingBody(t *testing.T) {
	h := &api.Handler{DB: nil}

	payload := map[string]interface{}{"entity_type": "client", "entity_id": 1}
	b, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/comments", bytes.NewReader(b))
	w := httptest.NewRecorder()

	h.CreateComment(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestListComments_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &api.Handler{DB: db}

	now := time.Now()
	rows := sqlmock.NewRows([]string{"id", "author", "body", "metadata", "created_at", "updated_at"}).
		AddRow(1, sql.NullString{String: "Sam", Valid: true}, "hi", `{"k":"v"}`, now, now).
		AddRow(2, sql.NullString{Valid: false}, "hello", nil, now, now)

	mock.ExpectQuery("SELECT id, author, body, metadata, created_at, updated_at").WillReturnRows(rows)

	req := httptest.NewRequest(http.MethodGet, "/api/comments?entity_type=client&entity_id=42", nil)
	w := httptest.NewRecorder()

	h.ListComments(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp []api.CommentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if len(resp) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(resp))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestDeleteComment_SuccessAndNotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &api.Handler{DB: db}

	// success path
	mock.ExpectExec("DELETE FROM comments").WithArgs(1).WillReturnResult(sqlmock.NewResult(0, 1))
	req := httptest.NewRequest(http.MethodDelete, "/api/comments/1", nil)
	w := httptest.NewRecorder()
	h.DeleteComment(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}

	// not found path
	mock.ExpectExec("DELETE FROM comments").WithArgs(99).WillReturnResult(sqlmock.NewResult(0, 0))
	req2 := httptest.NewRequest(http.MethodDelete, "/api/comments/99", nil)
	w2 := httptest.NewRecorder()
	h.DeleteComment(w2, req2)
	if w2.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w2.Code)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestListComments_BadParams(t *testing.T) {
	h := &api.Handler{DB: nil}
	req := httptest.NewRequest(http.MethodGet, "/api/comments", nil)
	w := httptest.NewRecorder()
	h.ListComments(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing params, got %d", w.Code)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/comments?entity_type=client&entity_id=not-an-int", nil)
	w2 := httptest.NewRecorder()
	h.ListComments(w2, req2)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid id, got %d", w2.Code)
	}
}

func TestListComments_Empty(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &api.Handler{DB: db}

	rows := sqlmock.NewRows([]string{"id", "author", "body", "metadata", "created_at", "updated_at"})
	mock.ExpectQuery("SELECT id, author, body, metadata, created_at, updated_at").WillReturnRows(rows)

	req := httptest.NewRequest(http.MethodGet, "/api/comments?entity_type=client&entity_id=42", nil)
	w := httptest.NewRecorder()
	h.ListComments(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp []api.CommentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if len(resp) != 0 {
		t.Fatalf("expected 0 comments, got %d", len(resp))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestDeleteComment_DBError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &api.Handler{DB: db}

	mock.ExpectExec("DELETE FROM comments").WithArgs(1).WillReturnError(sql.ErrConnDone)
	req := httptest.NewRequest(http.MethodDelete, "/api/comments/1", nil)
	w := httptest.NewRecorder()
	h.DeleteComment(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
