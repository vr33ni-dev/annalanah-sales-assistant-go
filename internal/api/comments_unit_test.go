package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/domain"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/store"
)

// ── UpdateComment ─────────────────────────────────────────────────────────────

func TestUpdateComment_BadJSON(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := httptest.NewRequest(http.MethodPatch, "/api/comments/1", bytes.NewReader([]byte("{bad")))
	w := httptest.NewRecorder()
	h.UpdateComment(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpdateComment_NoFields(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := httptest.NewRequest(http.MethodPatch, "/api/comments/1", bytes.NewReader([]byte(`{}`)))
	w := httptest.NewRecorder()
	h.UpdateComment(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpdateComment_Success(t *testing.T) {
	now := time.Now()
	meta := map[string]interface{}{"m": "v"}
	h := &Handler{store: &mockStore{
		updateComment: func(id int, author *string, body *string, metadata *map[string]interface{}) (domain.Comment, error) {
			return domain.Comment{
				ID:         1,
				EntityType: "client",
				EntityID:   42,
				Author:     strPtr("Bob"),
				Body:       "updated body",
				Metadata:   meta,
				CreatedAt:  now,
				UpdatedAt:  now,
			}, nil
		},
	}}

	payload := map[string]interface{}{"body": "updated body", "metadata": map[string]string{"m": "v"}}
	b, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPatch, "/api/comments/1", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.UpdateComment(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp CommentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if resp.Body != "updated body" {
		t.Fatalf("unexpected body: %s", resp.Body)
	}
}

func TestUpdateComment_NotFound(t *testing.T) {
	h := &Handler{store: &mockStore{
		updateComment: func(int, *string, *string, *map[string]interface{}) (domain.Comment, error) {
			return domain.Comment{}, store.ErrNotFound
		},
	}}

	payload := map[string]string{"body": "x"}
	b, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPatch, "/api/comments/99", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.UpdateComment(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestUpdateComment_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		updateComment: func(int, *string, *string, *map[string]interface{}) (domain.Comment, error) {
			return domain.Comment{}, errors.New("db down")
		},
	}}

	payload := map[string]string{"body": "x"}
	b, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPatch, "/api/comments/1", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.UpdateComment(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

// ── CreateComment ─────────────────────────────────────────────────────────────

func TestCreateComment_Success(t *testing.T) {
	now := time.Now()
	h := &Handler{store: &mockStore{
		createComment: func(entityType string, entityID int, author *string, body string, metadata map[string]interface{}) (domain.Comment, error) {
			return domain.Comment{
				ID:         1,
				EntityType: entityType,
				EntityID:   entityID,
				ClientID:   &entityID,
				Author:     author,
				Body:       body,
				Metadata:   metadata,
				CreatedAt:  now,
				UpdatedAt:  now,
			}, nil
		},
	}}

	payload := map[string]interface{}{
		"entity_type": "client",
		"entity_id":   42,
		"author":      "Alice",
		"body":        "hello world",
		"metadata":    map[string]string{"m": "v"},
	}
	b, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/comments", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.CreateComment(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d; body=%s", w.Code, w.Body.String())
	}
	var resp CommentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if resp.ID != 1 {
		t.Fatalf("unexpected id: %d", resp.ID)
	}
}

func TestCreateComment_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		createComment: func(string, int, *string, string, map[string]interface{}) (domain.Comment, error) {
			return domain.Comment{}, errors.New("db down")
		},
	}}

	payload := map[string]interface{}{"entity_type": "client", "entity_id": 42, "author": "Alice", "body": "hello"}
	b, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/comments", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.CreateComment(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestCreateComment_ContentAlias(t *testing.T) {
	now := time.Now()
	var gotBody string
	h := &Handler{store: &mockStore{
		createComment: func(_ string, _ int, _ *string, body string, _ map[string]interface{}) (domain.Comment, error) {
			gotBody = body
			return domain.Comment{ID: 5, EntityType: "client", EntityID: 7, Body: body, CreatedAt: now, UpdatedAt: now}, nil
		},
	}}

	payload := map[string]interface{}{"entity_type": "client", "entity_id": 7, "author": "Eve", "content": "alias body"}
	b, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/comments", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.CreateComment(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d; body=%s", w.Code, w.Body.String())
	}
	if gotBody != "alias body" {
		t.Fatalf("expected 'alias body', got %q", gotBody)
	}
}

func TestCreateComment_MissingBody(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	payload := map[string]interface{}{"entity_type": "client", "entity_id": 1}
	b, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/comments", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.CreateComment(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// ── ListComments ──────────────────────────────────────────────────────────────

func TestListComments_Success(t *testing.T) {
	now := time.Now()
	clientID := 5
	h := &Handler{store: &mockStore{
		listCommentsByEntity: func(entityType string, entityID int) ([]domain.Comment, error) {
			return []domain.Comment{
				{ID: 1, EntityType: entityType, EntityID: entityID, ClientID: &clientID, Author: strPtr("Sam"), Body: "hi", Metadata: map[string]interface{}{"k": "v"}, CreatedAt: now, UpdatedAt: now},
				{ID: 2, EntityType: entityType, EntityID: entityID, Body: "hello", CreatedAt: now, UpdatedAt: now},
			}, nil
		},
	}}

	req := httptest.NewRequest(http.MethodGet, "/api/comments?entity_type=client&entity_id=42", nil)
	w := httptest.NewRecorder()
	h.ListComments(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp []CommentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if len(resp) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(resp))
	}
}

func TestListComments_BadParams(t *testing.T) {
	h := &Handler{store: &mockStore{}}

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
	h := &Handler{store: &mockStore{
		listCommentsByEntity: func(string, int) ([]domain.Comment, error) {
			return nil, nil
		},
	}}

	req := httptest.NewRequest(http.MethodGet, "/api/comments?entity_type=client&entity_id=42", nil)
	w := httptest.NewRecorder()
	h.ListComments(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp []CommentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if len(resp) != 0 {
		t.Fatalf("expected 0 comments, got %d", len(resp))
	}
}

func TestListComments_ZeroTimestamps(t *testing.T) {
	h := &Handler{store: &mockStore{
		listCommentsByEntity: func(entityType string, entityID int) ([]domain.Comment, error) {
			return []domain.Comment{
				{ID: 1, EntityType: entityType, EntityID: entityID, Body: "hi"},
				// CreatedAt and UpdatedAt left as zero time
			}, nil
		},
	}}

	req := httptest.NewRequest(http.MethodGet, "/api/comments?entity_type=client&entity_id=42", nil)
	w := httptest.NewRecorder()
	h.ListComments(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body=%s", w.Code, w.Body.String())
	}
	var resp []CommentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if len(resp) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(resp))
	}
	if resp[0].CreatedAt != "" || resp[0].UpdatedAt != "" {
		t.Fatalf("expected empty timestamps for zero time, got created_at=%q updated_at=%q", resp[0].CreatedAt, resp[0].UpdatedAt)
	}
}

// ── DeleteComment ─────────────────────────────────────────────────────────────

func TestDeleteComment_Success(t *testing.T) {
	h := &Handler{store: &mockStore{
		deleteComment: func(id int) error { return nil },
	}}
	req := httptest.NewRequest(http.MethodDelete, "/api/comments/1", nil)
	w := httptest.NewRecorder()
	h.DeleteComment(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

func TestDeleteComment_NotFound(t *testing.T) {
	h := &Handler{store: &mockStore{
		deleteComment: func(int) error { return store.ErrNotFound },
	}}
	req := httptest.NewRequest(http.MethodDelete, "/api/comments/99", nil)
	w := httptest.NewRecorder()
	h.DeleteComment(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestDeleteComment_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		deleteComment: func(int) error { return errors.New("db down") },
	}}
	req := httptest.NewRequest(http.MethodDelete, "/api/comments/1", nil)
	w := httptest.NewRecorder()
	h.DeleteComment(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

