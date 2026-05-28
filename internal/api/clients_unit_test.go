package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/domain"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/store"
)

// ── ListClients ───────────────────────────────────────────────────────────────

func TestListClients_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		listClients: func(_ context.Context, _ bool) ([]domain.ClientRow, error) {
			return nil, errors.New("db down")
		},
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/clients", nil)
	w := httptest.NewRecorder()
	h.ListClients(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestListClients_Success(t *testing.T) {
	now := time.Now()
	h := &Handler{store: &mockStore{
		listClients: func(_ context.Context, _ bool) ([]domain.ClientRow, error) {
			return []domain.ClientRow{
				{
					ID:     1,
					Name:   "Alice",
					Email:  "alice@example.com",
					Status: "active",
					Comments: []domain.Comment{
						{ID: 10, EntityType: "client", EntityID: 1, Body: "hi", CreatedAt: now, UpdatedAt: now},
					},
				},
			}, nil
		},
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/clients", nil)
	w := httptest.NewRecorder()
	h.ListClients(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp []map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if len(resp) != 1 {
		t.Fatalf("expected 1 client, got %d", len(resp))
	}
	if resp[0]["name"] != "Alice" {
		t.Fatalf("unexpected name: %v", resp[0]["name"])
	}
}

// ── CreateClient ──────────────────────────────────────────────────────────────

func TestCreateClient_BadJSON(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := httptest.NewRequest(http.MethodPost, "/api/clients", bytes.NewReader([]byte("{bad")))
	w := httptest.NewRecorder()
	h.CreateClient(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCreateClient_MissingStatus(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	b, _ := json.Marshal(map[string]string{"name": "Bob", "email": "bob@example.com"})
	req := httptest.NewRequest(http.MethodPost, "/api/clients", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.CreateClient(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCreateClient_DuplicateEmail(t *testing.T) {
	h := &Handler{store: &mockStore{
		insertClient: func(_ context.Context, _, _, _, _ string, _ *int, _ string) (int, error) {
			return 0, store.ErrDuplicateEmail
		},
	}}
	b, _ := json.Marshal(map[string]string{"name": "Bob", "email": "bob@example.com", "source": "organic", "status": "active"})
	req := httptest.NewRequest(http.MethodPost, "/api/clients", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.CreateClient(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
}

func TestCreateClient_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		insertClient: func(_ context.Context, _, _, _, _ string, _ *int, _ string) (int, error) {
			return 0, errors.New("db down")
		},
	}}
	b, _ := json.Marshal(map[string]string{"name": "Bob", "email": "bob@example.com", "source": "organic", "status": "active"})
	req := httptest.NewRequest(http.MethodPost, "/api/clients", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.CreateClient(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestCreateClient_Success(t *testing.T) {
	h := &Handler{store: &mockStore{
		insertClient: func(_ context.Context, _, _, _, _ string, _ *int, _ string) (int, error) {
			return 42, nil
		},
	}}
	b, _ := json.Marshal(map[string]string{"name": "Bob", "email": "BOB@Example.com", "source": "organic", "status": "active"})
	req := httptest.NewRequest(http.MethodPost, "/api/clients", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.CreateClient(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if resp["email"] != "bob@example.com" {
		t.Fatalf("email not lowercased: %v", resp["email"])
	}
}

// ── DeleteClient ──────────────────────────────────────────────────────────────

func TestDeleteClient_InvalidID(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := httptest.NewRequest(http.MethodDelete, "/api/clients/abc", nil)
	w := httptest.NewRecorder()
	h.DeleteClient(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestDeleteClient_NotFound(t *testing.T) {
	h := &Handler{store: &mockStore{
		deleteClientWithLeadReset: func(_ context.Context, _ int) (bool, error) {
			return false, nil
		},
	}}
	req := httptest.NewRequest(http.MethodDelete, "/api/clients/99", nil)
	w := httptest.NewRecorder()
	h.DeleteClient(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestDeleteClient_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		deleteClientWithLeadReset: func(_ context.Context, _ int) (bool, error) {
			return false, errors.New("db down")
		},
	}}
	req := httptest.NewRequest(http.MethodDelete, "/api/clients/1", nil)
	w := httptest.NewRecorder()
	h.DeleteClient(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestDeleteClient_Success(t *testing.T) {
	h := &Handler{store: &mockStore{
		deleteClientWithLeadReset: func(_ context.Context, _ int) (bool, error) {
			return true, nil
		},
	}}
	req := httptest.NewRequest(http.MethodDelete, "/api/clients/1", nil)
	w := httptest.NewRecorder()
	h.DeleteClient(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

// ── UpdateClient ──────────────────────────────────────────────────────────────

func TestUpdateClient_InvalidID(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := httptest.NewRequest(http.MethodPatch, "/api/clients/abc", bytes.NewReader([]byte(`{}`)))
	w := httptest.NewRecorder()
	h.UpdateClient(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpdateClient_EmptyBody(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := httptest.NewRequest(http.MethodPatch, "/api/clients/1", bytes.NewReader([]byte{}))
	w := httptest.NewRecorder()
	h.UpdateClient(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpdateClient_BadJSON(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := httptest.NewRequest(http.MethodPatch, "/api/clients/1", bytes.NewReader([]byte("{bad")))
	w := httptest.NewRecorder()
	h.UpdateClient(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpdateClient_InvalidCompletedAt(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	b, _ := json.Marshal(map[string]string{
		"name": "Alice", "email": "alice@example.com", "status": "inactive",
		"completed_at": "not-a-date",
	})
	req := httptest.NewRequest(http.MethodPatch, "/api/clients/1", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.UpdateClient(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpdateClient_DuplicateEmail(t *testing.T) {
	h := &Handler{store: &mockStore{
		updateClientFields: func(_ context.Context, _ int, _, _, _, _, _ string, _ *int, _ bool, _ *time.Time) error {
			return store.ErrDuplicateEmail
		},
	}}
	b, _ := json.Marshal(map[string]string{"name": "Alice", "email": "alice@example.com", "status": "active"})
	req := httptest.NewRequest(http.MethodPatch, "/api/clients/1", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.UpdateClient(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
}

func TestUpdateClient_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		updateClientFields: func(_ context.Context, _ int, _, _, _, _, _ string, _ *int, _ bool, _ *time.Time) error {
			return errors.New("db down")
		},
	}}
	b, _ := json.Marshal(map[string]string{"name": "Alice", "email": "alice@example.com", "status": "active"})
	req := httptest.NewRequest(http.MethodPatch, "/api/clients/1", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.UpdateClient(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestUpdateClient_Success(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	b, _ := json.Marshal(map[string]string{"name": "Alice", "email": "alice@example.com", "status": "active"})
	req := httptest.NewRequest(http.MethodPatch, "/api/clients/1", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.UpdateClient(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

func TestUpdateClient_ClearedSourceStageID_CascadesClear(t *testing.T) {
	cascadeCalled := false
	h := &Handler{store: &mockStore{
		clearClientSalesProcessStageID: func(_ context.Context, _ int) error {
			cascadeCalled = true
			return nil
		},
	}}
	// Explicitly send source_stage_id: null to trigger the cascade
	b := []byte(`{"name":"Alice","email":"alice@example.com","status":"active","source_stage_id":null}`)
	req := httptest.NewRequest(http.MethodPatch, "/api/clients/1", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.UpdateClient(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
	if !cascadeCalled {
		t.Fatal("expected ClearClientSalesProcessStageID to be called")
	}
}
