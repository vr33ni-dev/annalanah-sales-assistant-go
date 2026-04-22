package api_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
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
			linked_lead_id INTEGER,
			participant_name TEXT,
			participant_email TEXT,
			participant_phone TEXT,
			attended BOOLEAN
		);`,
		`CREATE TABLE stage_client_assignments (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			client_id INTEGER,
			stage_id INTEGER
		);`,
		`CREATE TABLE sales_process (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			client_id INTEGER,
			stage TEXT,
			closed BOOLEAN,
			stage_id INTEGER
		);`,
		`CREATE TABLE contracts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			client_id INTEGER,
			sales_process_id INTEGER,
			revenue_total REAL
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
	if got[0].MonetaryMode != "brutto" {
		t.Fatalf("expected monetary_mode brutto, got %q", got[0].MonetaryMode)
	}
}

func TestListStages_ComputesPerformanceMetrics(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	createStageSchema(db, t)

	_, _ = db.Exec(`INSERT INTO sales_process (id, client_id, stage, closed, stage_id) VALUES (1, 100, 'closed', 1, 1)`)
	_, _ = db.Exec(`INSERT INTO sales_process (id, client_id, stage, closed, stage_id) VALUES (2, 101, 'follow_up', 0, 1)`)
	_, _ = db.Exec(`INSERT INTO contracts (client_id, sales_process_id, revenue_total) VALUES (100, 1, 2400)`)
	_, _ = db.Exec(`INSERT INTO contracts (client_id, sales_process_id, revenue_total) VALUES (101, 2, 9999)`)

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
	if len(got) != 1 {
		t.Fatalf("expected one stage, got %+v", got)
	}

	stage := got[0]
	if stage.ClosedContracts == nil || *stage.ClosedContracts != 1 {
		t.Fatalf("expected 1 closed contract, got %+v", stage.ClosedContracts)
	}
	if stage.ActualRevenue == nil || *stage.ActualRevenue != 2400 {
		t.Fatalf("expected actual revenue 2400, got %+v", stage.ActualRevenue)
	}
	if stage.AttendanceRate == nil || *stage.AttendanceRate != 80 {
		t.Fatalf("expected attendance rate 80, got %+v", stage.AttendanceRate)
	}
	if stage.ClosingRate == nil || *stage.ClosingRate != 12.5 {
		t.Fatalf("expected closing rate 12.5, got %+v", stage.ClosingRate)
	}
	if stage.ROI == nil || *stage.ROI != 0.48 {
		t.Fatalf("expected roi 0.48, got %+v", stage.ROI)
	}
}

func TestListStages_RoundsPercentageMetrics(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	createStageSchema(db, t)

	_, _ = db.Exec(`UPDATE stages SET registrations = 45, participants = 30 WHERE id = 1`)
	_, _ = db.Exec(`INSERT INTO sales_process (id, client_id, stage, closed, stage_id) VALUES (1, 100, 'closed', 1, 1)`)
	_, _ = db.Exec(`INSERT INTO sales_process (id, client_id, stage, closed, stage_id) VALUES (2, 101, 'closed', 1, 1)`)
	_, _ = db.Exec(`INSERT INTO contracts (client_id, sales_process_id, revenue_total) VALUES (100, 1, 1000)`)
	_, _ = db.Exec(`INSERT INTO contracts (client_id, sales_process_id, revenue_total) VALUES (101, 2, 500)`)

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
	if len(got) != 1 {
		t.Fatalf("expected one stage, got %+v", got)
	}

	stage := got[0]
	if stage.ClosingRate == nil || math.Abs(*stage.ClosingRate-6.7) > 0.000001 {
		t.Fatalf("expected closing rate 6.7, got %+v", stage.ClosingRate)
	}
	if stage.AttendanceRate == nil || math.Abs(*stage.AttendanceRate-66.7) > 0.000001 {
		t.Fatalf("expected attendance rate 66.7, got %+v", stage.AttendanceRate)
	}
	if stage.ROI == nil || math.Abs(*stage.ROI-0.30) > 0.000001 {
		t.Fatalf("expected roi 0.30, got %+v", stage.ROI)
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

func TestDeleteStage(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	createStageSchema(db, t)
	_, _ = db.Exec(`INSERT INTO stage_participants (stage_id, participant_name) VALUES (1, 'Laura Beispiel')`)
	_, _ = db.Exec(`INSERT INTO stage_client_assignments (client_id, stage_id) VALUES (99, 1)`)

	h := &api.Handler{DB: db}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")
	req := httptest.NewRequest(http.MethodDelete, "/api/stages/1", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	h.DeleteStage(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	var stagesCount int
	_ = db.QueryRow(`SELECT COUNT(*) FROM stages WHERE id = 1`).Scan(&stagesCount)
	if stagesCount != 0 {
		t.Fatalf("expected stage to be deleted, got %d rows", stagesCount)
	}

	var participantsCount int
	_ = db.QueryRow(`SELECT COUNT(*) FROM stage_participants WHERE stage_id = 1`).Scan(&participantsCount)
	if participantsCount != 0 {
		t.Fatalf("expected stage participants to be deleted, got %d rows", participantsCount)
	}

	var assignmentsCount int
	_ = db.QueryRow(`SELECT COUNT(*) FROM stage_client_assignments WHERE stage_id = 1`).Scan(&assignmentsCount)
	if assignmentsCount != 0 {
		t.Fatalf("expected stage assignments to be deleted, got %d rows", assignmentsCount)
	}
}

func TestDeleteStage_InvalidID(t *testing.T) {
	h := &api.Handler{}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "abc")
	req := httptest.NewRequest(http.MethodDelete, "/api/stages/abc", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	h.DeleteStage(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestDeleteStage_NotFound(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	createStageSchema(db, t)
	h := &api.Handler{DB: db}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "999")
	req := httptest.NewRequest(http.MethodDelete, "/api/stages/999", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	h.DeleteStage(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
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
		"participant_name":  "Laura Beispiel",
		"participant_email": "laura@example.com",
		"participant_phone": "01234",
		"attended":          true,
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
	_ = db.QueryRow(`SELECT COUNT(*) FROM stage_participants WHERE participant_name='Laura Beispiel'`).Scan(&count)
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
		"linked_client_id": 42,
		"attended":         false,
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

func TestUpdateStageInfo(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	createStageSchema(db, t)
	h := &api.Handler{DB: db}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")

	body := map[string]any{"name": "Updated Stage", "ad_budget": 7500}
	b, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPatch, "/api/stages/1", bytes.NewReader(b))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	h.UpdateStageInfo(w, req)
	resp := w.Result()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
	var name string
	var budget float64
	_ = db.QueryRow(`SELECT name, ad_budget FROM stages WHERE id=1`).Scan(&name, &budget)
	if name != "Updated Stage" || budget != 7500 {
		t.Fatalf("expected updated values, got name=%s, ad_budget=%f", name, budget)
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
		t.Fatalf("expected 400 for missing linked_client_id/participant_name, got %d", resp.StatusCode)
	}
}

func TestListStageParticipants_RealTimestampRows(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &api.Handler{DB: db}
	createdAt, _ := time.Parse(time.RFC3339, "2026-03-07T15:33:08Z")
	rows := sqlmock.NewRows([]string{
		"id",
		"stage_id",
		"linked_client_id",
		"linked_lead_id",
		"name",
		"email",
		"phone",
		"attended",
		"created_at",
	}).AddRow(1, 1, nil, nil, "Jane Doe", "jane@example.com", "+491234", true, createdAt)

	mock.ExpectQuery("FROM stage_participants sp").WithArgs(1, 25, 0).WillReturnRows(rows)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")
	req := httptest.NewRequest(http.MethodGet, "/api/stages/1/participants", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	h.ListStageParticipants(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body=%s", w.Code, w.Body.String())
	}

	var got []api.StageParticipant
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 participant, got %d", len(got))
	}
	if got[0].CreatedAt == nil || *got[0].CreatedAt != "2026-03-07T15:33:08Z" {
		t.Fatalf("unexpected created_at: %#v", got[0].CreatedAt)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// --- helpers for pointer types ---
func ptrF(f float64) *float64 { return &f }
