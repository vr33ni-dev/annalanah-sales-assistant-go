package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/domain"
)

// ── ListLeads ─────────────────────────────────────────────────────────────────

func TestListLeads_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		listLeads: func(_ context.Context) ([]domain.Lead, error) {
			return nil, errors.New("db down")
		},
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/leads", nil)
	w := httptest.NewRecorder()
	h.ListLeads(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestListLeads_Success(t *testing.T) {
	h := &Handler{store: &mockStore{
		listLeads: func(_ context.Context) ([]domain.Lead, error) {
			return []domain.Lead{
				{ID: 1, Name: "Alice", Email: "alice@test.com", Source: "organic"},
			}, nil
		},
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/leads", nil)
	w := httptest.NewRecorder()
	h.ListLeads(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var out []LeadResponse
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != 1 || out[0].Name != "Alice" {
		t.Fatalf("unexpected response: %+v", out)
	}
}

// ── CreateLead ────────────────────────────────────────────────────────────────

func TestCreateLead_BadJSON(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := httptest.NewRequest(http.MethodPost, "/api/leads", bytes.NewReader([]byte("{bad")))
	w := httptest.NewRecorder()
	h.CreateLead(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCreateLead_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		createLead: func(_ context.Context, _ string, _, _ *string, _ string, _ *int) (domain.Lead, bool, error) {
			return domain.Lead{}, false, errors.New("db down")
		},
	}}
	b, _ := json.Marshal(map[string]string{"name": "Bob", "email": "b@test.com", "source": "organic"})
	req := httptest.NewRequest(http.MethodPost, "/api/leads", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.CreateLead(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestCreateLead_NewLead_Returns201(t *testing.T) {
	h := &Handler{store: &mockStore{
		createLead: func(_ context.Context, name string, email, _ *string, source string, _ *int) (domain.Lead, bool, error) {
			return domain.Lead{ID: 10, Name: name, Email: *email, Source: source}, false, nil
		},
	}}
	b, _ := json.Marshal(map[string]string{"name": "Bob", "email": "b@test.com", "source": "organic"})
	req := httptest.NewRequest(http.MethodPost, "/api/leads", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.CreateLead(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}
}

func TestCreateLead_ExistingLead_Returns200(t *testing.T) {
	h := &Handler{store: &mockStore{
		createLead: func(_ context.Context, name string, _, _ *string, source string, _ *int) (domain.Lead, bool, error) {
			return domain.Lead{ID: 5, Name: name, Source: source}, true, nil
		},
	}}
	b, _ := json.Marshal(map[string]string{"name": "Dup", "email": "dup@test.com", "source": "organic"})
	req := httptest.NewRequest(http.MethodPost, "/api/leads", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.CreateLead(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestCreateLead_EmailLowercased(t *testing.T) {
	var capturedEmail string
	h := &Handler{store: &mockStore{
		createLead: func(_ context.Context, _ string, email, _ *string, source string, _ *int) (domain.Lead, bool, error) {
			if email != nil {
				capturedEmail = *email
			}
			return domain.Lead{}, false, nil
		},
	}}
	b, _ := json.Marshal(map[string]string{"name": "Eve", "email": "EVE@Test.COM", "source": "organic"})
	req := httptest.NewRequest(http.MethodPost, "/api/leads", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.CreateLead(w, req)
	if capturedEmail != "eve@test.com" {
		t.Fatalf("expected email lowercased, got %q", capturedEmail)
	}
}

// ── UpdateLead ────────────────────────────────────────────────────────────────

func TestUpdateLead_InvalidID(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := chiReqWithID(http.MethodPatch, "/api/leads/abc", "abc", []byte(`{"name":"X"}`))
	w := httptest.NewRecorder()
	h.UpdateLead(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpdateLead_NoFields(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := chiReqWithID(http.MethodPatch, "/api/leads/1", "1", []byte(`{}`))
	w := httptest.NewRecorder()
	h.UpdateLead(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpdateLead_NotFound(t *testing.T) {
	h := &Handler{store: &mockStore{
		updateLead: func(_ context.Context, _ int, _, _, _, _ *string, _ *int) (domain.Lead, error) {
			return domain.Lead{}, errors.New("lead not found")
		},
	}}
	req := chiReqWithID(http.MethodPatch, "/api/leads/99", "99", []byte(`{"name":"X"}`))
	w := httptest.NewRecorder()
	h.UpdateLead(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestUpdateLead_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		updateLead: func(_ context.Context, _ int, _, _, _, _ *string, _ *int) (domain.Lead, error) {
			return domain.Lead{}, errors.New("db down")
		},
	}}
	req := chiReqWithID(http.MethodPatch, "/api/leads/1", "1", []byte(`{"name":"X"}`))
	w := httptest.NewRecorder()
	h.UpdateLead(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestUpdateLead_Success(t *testing.T) {
	h := &Handler{store: &mockStore{
		updateLead: func(_ context.Context, _ int, _, _, _, _ *string, _ *int) (domain.Lead, error) {
			return domain.Lead{ID: 1, Name: "Alice", Source: "organic"}, nil
		},
	}}
	req := chiReqWithID(http.MethodPatch, "/api/leads/1", "1", []byte(`{"name":"Alice"}`))
	w := httptest.NewRecorder()
	h.UpdateLead(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// ── DeleteLead ────────────────────────────────────────────────────────────────

func TestDeleteLead_InvalidID(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := chiReqWithID(http.MethodDelete, "/api/leads/abc", "abc", nil)
	w := httptest.NewRecorder()
	h.DeleteLead(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestDeleteLead_NotFound(t *testing.T) {
	h := &Handler{store: &mockStore{
		deleteLead: func(_ context.Context, _ int) error {
			return errors.New("lead not found")
		},
	}}
	req := chiReqWithID(http.MethodDelete, "/api/leads/9", "9", nil)
	w := httptest.NewRecorder()
	h.DeleteLead(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestDeleteLead_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		deleteLead: func(_ context.Context, _ int) error {
			return errors.New("db down")
		},
	}}
	req := chiReqWithID(http.MethodDelete, "/api/leads/1", "1", nil)
	w := httptest.NewRecorder()
	h.DeleteLead(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestDeleteLead_Success(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := chiReqWithID(http.MethodDelete, "/api/leads/3", "3", nil)
	w := httptest.NewRecorder()
	h.DeleteLead(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

// ── ConvertLead ───────────────────────────────────────────────────────────────

func TestConvertLead_InvalidID(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := chiReqWithID(http.MethodPost, "/api/leads/abc/convert", "abc", nil)
	w := httptest.NewRecorder()
	h.ConvertLead(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestConvertLead_NotFound(t *testing.T) {
	h := &Handler{store: &mockStore{
		convertLead: func(_ context.Context, _ int) (int, int, error) {
			return 0, 0, errors.New("lead not found")
		},
	}}
	req := chiReqWithID(http.MethodPost, "/api/leads/1/convert", "1", nil)
	w := httptest.NewRecorder()
	h.ConvertLead(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestConvertLead_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		convertLead: func(_ context.Context, _ int) (int, int, error) {
			return 0, 0, errors.New("db down")
		},
	}}
	req := chiReqWithID(http.MethodPost, "/api/leads/1/convert", "1", nil)
	w := httptest.NewRecorder()
	h.ConvertLead(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestConvertLead_Success(t *testing.T) {
	h := &Handler{store: &mockStore{
		convertLead: func(_ context.Context, _ int) (int, int, error) {
			return 11, 21, nil
		},
	}}
	req := chiReqWithID(http.MethodPost, "/api/leads/1/convert", "1", nil)
	w := httptest.NewRecorder()
	h.ConvertLead(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var out map[string]int
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["client_id"] != 11 || out["sales_process_id"] != 21 {
		t.Fatalf("unexpected ids: %+v", out)
	}
}
