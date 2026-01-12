package api_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
