package api_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	_ "github.com/mattn/go-sqlite3"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/api"
)

// --- Helper to init schema ---
func createStageSchema(db *sql.DB, t *testing.T) {
	stmts := []string{
		`CREATE TABLE stages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT,
			date TEXT,
			ad_budget REAL,
			registrations INTEGER,
			participants INTEGER
		);`,
		`CREATE TABLE stage_participants (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			stage_id INTEGER,
			linked_client_id INTEGER,
			lead_name TEXT,
			lead_email TEXT,
			lead_phone TEXT,
			attended BOOLEAN
		);`,
		`CREATE TABLE stage_client_assignments (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			client_id INTEGER,
			stage_id INTEGER
		);`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("schema creation failed: %v", err)
		}
	}
	_, _ = db.Exec(`INSERT INTO stages (name, date, ad_budget, registrations, participants)
	                VALUES ('Kickoff', '2025-01-01', 5000, 10, 8)`)
}

// --- Tests ---

func TestListStages(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	createStageSchema(db, t)

	h := &api.Handler{DB: db}

	req := httptest.NewRequest(http.MethodGet, "/api/stages", nil)
	w := httptest.NewRecorder()
	h.ListStages(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	var got []api.Stage
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(got) != 1 || got[0].Name != "Kickoff" {
		t.Fatalf("expected one stage named Kickoff, got %+v", got)
	}
}

func TestCreateStage(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	createStageSchema(db, t)

	h := &api.Handler{DB: db}

	stage := api.Stage{Name: "Follow-up", AdBudget: ptrF(1000)}
	body, _ := json.Marshal(stage)

	req := httptest.NewRequest(http.MethodPost, "/api/stages", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.CreateStage(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	var created api.Stage
	_ = json.NewDecoder(resp.Body).Decode(&created)
	if created.ID == 0 {
		t.Fatal("expected stage ID to be assigned")
	}

	var count int
	_ = db.QueryRow(`SELECT COUNT(*) FROM stages WHERE name='Follow-up'`).Scan(&count)
	if count != 1 {
		t.Fatalf("expected 1 Follow-up record, got %d", count)
	}
}

func TestAddStageParticipant_NewLead(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	createStageSchema(db, t)
	h := &api.Handler{DB: db}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")
	reqBody := map[string]any{
		"lead_name":  "Laura Beispiel",
		"lead_email": "laura@example.com",
		"lead_phone": "01234",
		"attended":   true,
	}
	b, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/stages/1/participants", bytes.NewReader(b))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	h.AddStageParticipant(w, req)
	resp := w.Result()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d", resp.StatusCode)
	}

	var count int
	_ = db.QueryRow(`SELECT COUNT(*) FROM stage_participants WHERE lead_name='Laura Beispiel'`).Scan(&count)
	if count != 1 {
		t.Fatalf("expected 1 participant, got %d", count)
	}
}

func TestAddStageParticipant_ExistingClient(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	createStageSchema(db, t)
	h := &api.Handler{DB: db}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")
	reqBody := map[string]any{
		"client_id": 42,
		"attended":  false,
	}
	b, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/stages/1/participants", bytes.NewReader(b))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	h.AddStageParticipant(w, req)
	resp := w.Result()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d", resp.StatusCode)
	}
}

func TestUpdateStageParticipant(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	createStageSchema(db, t)
	db.Exec(`INSERT INTO stage_participants (stage_id, attended) VALUES (1, false)`)

	h := &api.Handler{DB: db}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")
	rctx.URLParams.Add("participant_id", "1")

	body := map[string]any{"attended": true}
	b, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPatch, "/api/stages/1/participants/1", bytes.NewReader(b))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	h.UpdateStageParticipant(w, req)
	resp := w.Result()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}

	var attended bool
	_ = db.QueryRow(`SELECT attended FROM stage_participants WHERE id=1`).Scan(&attended)
	if !attended {
		t.Fatal("expected attended=true after update")
	}
}

func TestUpdateStageStats(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	createStageSchema(db, t)
	h := &api.Handler{DB: db}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")

	body := map[string]any{"registrations": 20, "participants": 15}
	b, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPatch, "/api/stages/1/stats", bytes.NewReader(b))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	h.UpdateStageStats(w, req)
	resp := w.Result()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}

	var regs, parts int
	_ = db.QueryRow(`SELECT registrations, participants FROM stages WHERE id=1`).Scan(&regs, &parts)
	if regs != 20 || parts != 15 {
		t.Fatalf("expected (20,15), got (%d,%d)", regs, parts)
	}
}

func TestAssignClientToStage(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	createStageSchema(db, t)
	h := &api.Handler{DB: db}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")
	body := map[string]any{"client_id": 99}
	b, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/stages/1/assign-client", bytes.NewReader(b))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	h.AssignClientToStage(w, req)
	resp := w.Result()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var count int
	_ = db.QueryRow(`SELECT COUNT(*) FROM stage_client_assignments WHERE client_id=99`).Scan(&count)
	if count != 1 {
		t.Fatalf("expected 1 assignment, got %d", count)
	}
}

func TestAddStageParticipant_MissingLeadAndClient(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	createStageSchema(db, t)
	h := &api.Handler{DB: db}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")
	b, _ := json.Marshal(map[string]any{"attended": true})

	req := httptest.NewRequest(http.MethodPost, "/api/stages/1/participants", bytes.NewReader(b))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	h.AddStageParticipant(w, req)
	resp := w.Result()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing client_id/lead_name, got %d", resp.StatusCode)
	}
}

// --- helpers for pointer types ---
func ptrF(f float64) *float64 { return &f }
