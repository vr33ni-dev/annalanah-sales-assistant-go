package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

// errTest is a simple sentinel error for use in tests.
type errTest string

func (e errTest) Error() string { return string(e) }
