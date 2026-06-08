package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/domain"
)

// chiReqWithTwoIDs builds a request with two chi URL params: "id" and "participant_id".
func chiReqWithTwoIDs(method, url, id, participantID string, body []byte) *http.Request {
	req := httptest.NewRequest(method, url, bytes.NewReader(body))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	rctx.URLParams.Add("participant_id", participantID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// ── ListStages ────────────────────────────────────────────────────────────────

func TestListStages_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		listStages: func() ([]domain.Stage, error) {
			return nil, errors.New("db down")
		},
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/stages", nil)
	w := httptest.NewRecorder()
	h.ListStages(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestListStages_Success(t *testing.T) {
	h := &Handler{store: &mockStore{
		listStages: func() ([]domain.Stage, error) {
			return []domain.Stage{{ID: 1, Name: "Webinar Jan"}}, nil
		},
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/stages", nil)
	w := httptest.NewRecorder()
	h.ListStages(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var out []domain.Stage
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != 1 || out[0].Name != "Webinar Jan" {
		t.Fatalf("unexpected response: %+v", out)
	}
}

// ── ListStageParticipants ─────────────────────────────────────────────────────

func TestListStageParticipants_InvalidID(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := chiReqWithID(http.MethodGet, "/api/stages/abc/participants", "abc", nil)
	w := httptest.NewRecorder()
	h.ListStageParticipants(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestListStageParticipants_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		listStageParticipants: func(_, _, _ int) ([]domain.StageParticipant, error) {
			return nil, errors.New("db down")
		},
	}}
	req := chiReqWithID(http.MethodGet, "/api/stages/1/participants", "1", nil)
	w := httptest.NewRecorder()
	h.ListStageParticipants(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestListStageParticipants_Success(t *testing.T) {
	h := &Handler{store: &mockStore{
		listStageParticipants: func(_, _, _ int) ([]domain.StageParticipant, error) {
			return []domain.StageParticipant{{ID: 1, ParticipantName: "Alice"}}, nil
		},
	}}
	req := chiReqWithID(http.MethodGet, "/api/stages/1/participants", "1", nil)
	w := httptest.NewRecorder()
	h.ListStageParticipants(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var out []domain.StageParticipant
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != 1 || out[0].ParticipantName != "Alice" {
		t.Fatalf("unexpected response: %+v", out)
	}
}

// ── CreateStage ───────────────────────────────────────────────────────────────

func TestCreateStage_BadJSON(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := httptest.NewRequest(http.MethodPost, "/api/stages", bytes.NewReader([]byte(`{bad`)))
	w := httptest.NewRecorder()
	h.CreateStage(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCreateStage_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		createStage: func(_ domain.Stage) (domain.Stage, error) {
			return domain.Stage{}, errors.New("db down")
		},
	}}
	b, _ := json.Marshal(map[string]string{"name": "Webinar"})
	req := httptest.NewRequest(http.MethodPost, "/api/stages", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.CreateStage(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestCreateStage_Success(t *testing.T) {
	h := &Handler{store: &mockStore{
		createStage: func(s domain.Stage) (domain.Stage, error) {
			s.ID = 5
			return s, nil
		},
	}}
	b, _ := json.Marshal(map[string]string{"name": "Webinar"})
	req := httptest.NewRequest(http.MethodPost, "/api/stages", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.CreateStage(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}
	var out map[string]int
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["id"] != 5 {
		t.Fatalf("expected id=5, got %v", out["id"])
	}
}

// ── DeleteStage ───────────────────────────────────────────────────────────────

func TestDeleteStage_InvalidID(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := chiReqWithID(http.MethodDelete, "/api/stages/abc", "abc", nil)
	w := httptest.NewRecorder()
	h.DeleteStage(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestDeleteStage_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		deleteStage: func(_ int) error { return errors.New("db down") },
	}}
	req := chiReqWithID(http.MethodDelete, "/api/stages/1", "1", nil)
	w := httptest.NewRecorder()
	h.DeleteStage(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestDeleteStage_Success(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := chiReqWithID(http.MethodDelete, "/api/stages/1", "1", nil)
	w := httptest.NewRecorder()
	h.DeleteStage(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

// ── AddStageParticipant ───────────────────────────────────────────────────────

func TestAddStageParticipant_BadJSON(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := chiReqWithID(http.MethodPost, "/api/stages/1/participants", "1", []byte(`{bad`))
	w := httptest.NewRecorder()
	h.AddStageParticipant(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAddStageParticipant_NoNameOrClientID(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := chiReqWithID(http.MethodPost, "/api/stages/1/participants", "1", []byte(`{}`))
	w := httptest.NewRecorder()
	h.AddStageParticipant(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAddStageParticipant_CreateAsLeadWithoutEmail(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	b, _ := json.Marshal(map[string]any{"participant_name": "Bob", "create_as_lead": true})
	req := chiReqWithID(http.MethodPost, "/api/stages/1/participants", "1", b)
	w := httptest.NewRecorder()
	h.AddStageParticipant(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAddStageParticipant_InsertLeadError(t *testing.T) {
	h := &Handler{store: &mockStore{
		insertLeadForStage: func(_, _, _ string, _ int) (int, error) {
			return 0, errors.New("db down")
		},
	}}
	b, _ := json.Marshal(map[string]any{
		"participant_name":  "Bob",
		"participant_email": "bob@test.com",
		"create_as_lead":    true,
	})
	req := chiReqWithID(http.MethodPost, "/api/stages/1/participants", "1", b)
	w := httptest.NewRecorder()
	h.AddStageParticipant(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestAddStageParticipant_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		addStageParticipant: func(_ int, _ domain.StageParticipant) (domain.StageParticipant, error) {
			return domain.StageParticipant{}, errors.New("db down")
		},
	}}
	b, _ := json.Marshal(map[string]any{"participant_name": "Bob"})
	req := chiReqWithID(http.MethodPost, "/api/stages/1/participants", "1", b)
	w := httptest.NewRecorder()
	h.AddStageParticipant(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestAddStageParticipant_Success(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	b, _ := json.Marshal(map[string]any{"participant_name": "Bob"})
	req := chiReqWithID(http.MethodPost, "/api/stages/1/participants", "1", b)
	w := httptest.NewRecorder()
	h.AddStageParticipant(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}
}

func TestAddStageParticipant_SuccessWithClientID(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	b, _ := json.Marshal(map[string]any{"linked_client_id": 42})
	req := chiReqWithID(http.MethodPost, "/api/stages/1/participants", "1", b)
	w := httptest.NewRecorder()
	h.AddStageParticipant(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}
}

// ── UpdateStageParticipant ────────────────────────────────────────────────────

func TestUpdateStageParticipant_InvalidStageID(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := chiReqWithTwoIDs(http.MethodPatch, "/api/stages/abc/participants/1", "abc", "1", []byte(`{}`))
	w := httptest.NewRecorder()
	h.UpdateStageParticipant(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpdateStageParticipant_InvalidParticipantID(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := chiReqWithTwoIDs(http.MethodPatch, "/api/stages/1/participants/abc", "1", "abc", []byte(`{}`))
	w := httptest.NewRecorder()
	h.UpdateStageParticipant(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpdateStageParticipant_BadJSON(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := chiReqWithTwoIDs(http.MethodPatch, "/api/stages/1/participants/1", "1", "1", []byte(`{bad`))
	w := httptest.NewRecorder()
	h.UpdateStageParticipant(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpdateStageParticipant_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		updateStageParticipant: func(_ int, _ domain.StageParticipant) error {
			return errors.New("db down")
		},
	}}
	req := chiReqWithTwoIDs(http.MethodPatch, "/api/stages/1/participants/2", "1", "2", []byte(`{}`))
	w := httptest.NewRecorder()
	h.UpdateStageParticipant(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestUpdateStageParticipant_Success(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	attended := true
	b, _ := json.Marshal(map[string]any{"attended": attended})
	req := chiReqWithTwoIDs(http.MethodPatch, "/api/stages/1/participants/2", "1", "2", b)
	w := httptest.NewRecorder()
	h.UpdateStageParticipant(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

// ── DeleteStageParticipant ────────────────────────────────────────────────────

func TestDeleteStageParticipant_InvalidStageID(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := chiReqWithTwoIDs(http.MethodDelete, "/api/stages/abc/participants/1", "abc", "1", nil)
	w := httptest.NewRecorder()
	h.DeleteStageParticipant(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestDeleteStageParticipant_InvalidParticipantID(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := chiReqWithTwoIDs(http.MethodDelete, "/api/stages/1/participants/abc", "1", "abc", nil)
	w := httptest.NewRecorder()
	h.DeleteStageParticipant(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestDeleteStageParticipant_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		deleteStageParticipant: func(_, _ int) error { return errors.New("db down") },
	}}
	req := chiReqWithTwoIDs(http.MethodDelete, "/api/stages/1/participants/2", "1", "2", nil)
	w := httptest.NewRecorder()
	h.DeleteStageParticipant(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestDeleteStageParticipant_Success(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := chiReqWithTwoIDs(http.MethodDelete, "/api/stages/1/participants/2", "1", "2", nil)
	w := httptest.NewRecorder()
	h.DeleteStageParticipant(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

// ── UpdateStageStats ──────────────────────────────────────────────────────────

func TestUpdateStageStats_InvalidID(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := chiReqWithID(http.MethodPatch, "/api/stages/abc/stats", "abc", []byte(`{}`))
	w := httptest.NewRecorder()
	h.UpdateStageStats(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpdateStageStats_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		updateStageStats: func(_ int, _, _ *int) error { return errors.New("db down") },
	}}
	b, _ := json.Marshal(map[string]int{"registrations": 10})
	req := chiReqWithID(http.MethodPatch, "/api/stages/1/stats", "1", b)
	w := httptest.NewRecorder()
	h.UpdateStageStats(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestUpdateStageStats_Success(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	b, _ := json.Marshal(map[string]int{"registrations": 10, "participants": 8})
	req := chiReqWithID(http.MethodPatch, "/api/stages/1/stats", "1", b)
	w := httptest.NewRecorder()
	h.UpdateStageStats(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

// ── UpdateStageInfo ───────────────────────────────────────────────────────────

func TestUpdateStageInfo_InvalidID(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := chiReqWithID(http.MethodPatch, "/api/stages/abc", "abc", []byte(`{}`))
	w := httptest.NewRecorder()
	h.UpdateStageInfo(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpdateStageInfo_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		updateStageInfo: func(_ int, _, _ *string, _ *float64) error { return errors.New("db down") },
	}}
	b, _ := json.Marshal(map[string]string{"name": "New Name"})
	req := chiReqWithID(http.MethodPatch, "/api/stages/1", "1", b)
	w := httptest.NewRecorder()
	h.UpdateStageInfo(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestUpdateStageInfo_Success(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	b, _ := json.Marshal(map[string]string{"name": "New Name", "date": "2026-03-01"})
	req := chiReqWithID(http.MethodPatch, "/api/stages/1", "1", b)
	w := httptest.NewRecorder()
	h.UpdateStageInfo(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

// ── AssignClientToStage ───────────────────────────────────────────────────────

func TestAssignClientToStage_InvalidID(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := chiReqWithID(http.MethodPost, "/api/stages/abc/assign-client", "abc", []byte(`{"client_id":1}`))
	w := httptest.NewRecorder()
	h.AssignClientToStage(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAssignClientToStage_MissingClientID(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := chiReqWithID(http.MethodPost, "/api/stages/1/assign-client", "1", []byte(`{}`))
	w := httptest.NewRecorder()
	h.AssignClientToStage(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAssignClientToStage_StoreError(t *testing.T) {
	h := &Handler{store: &mockStore{
		assignClientToStage: func(_, _ int) error { return errors.New("db down") },
	}}
	req := chiReqWithID(http.MethodPost, "/api/stages/1/assign-client", "1", []byte(`{"client_id":5}`))
	w := httptest.NewRecorder()
	h.AssignClientToStage(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestAssignClientToStage_Success(t *testing.T) {
	h := &Handler{store: &mockStore{}}
	req := chiReqWithID(http.MethodPost, "/api/stages/1/assign-client", "1", []byte(`{"client_id":5}`))
	w := httptest.NewRecorder()
	h.AssignClientToStage(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}
}
