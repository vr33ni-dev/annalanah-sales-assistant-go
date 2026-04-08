package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

func TestParseMonthParam(t *testing.T) {
	if _, err := parseMonthParam("2026-02"); err != nil {
		t.Fatalf("expected valid month, got err=%v", err)
	}
	if _, err := parseMonthParam(""); err != nil {
		t.Fatalf("expected empty month to be accepted, got err=%v", err)
	}
	if _, err := parseMonthParam("2026/02"); err == nil {
		t.Fatalf("expected invalid month error")
	}
}

func TestParseDateStringAndHelpers(t *testing.T) {
	if _, ok := parseDateString("2026-01-05"); !ok {
		t.Fatalf("expected date-only parse ok")
	}
	if _, ok := parseDateString("2026-01-05 10:20:30"); !ok {
		t.Fatalf("expected datetime parse ok")
	}
	if _, ok := parseDateString(time.Now().Format(time.RFC3339)); !ok {
		t.Fatalf("expected rfc3339 parse ok")
	}
	if _, ok := parseDateString("not-a-date"); ok {
		t.Fatalf("expected invalid date parse to fail")
	}

	if got := asString(nil); got != "" {
		t.Fatalf("expected empty string for nil, got %q", got)
	}
	if got := asString(42); got != "42" {
		t.Fatalf("expected fmt string conversion, got %q", got)
	}
}

func TestBuildMonthRangeInclusive_EndBeforeStart(t *testing.T) {
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if got := buildMonthRangeInclusive(start, end); got != nil {
		t.Fatalf("expected nil range when end before start, got %v", got)
	}
}

func TestParseDateString_FallbackPath(t *testing.T) {
	// String with T separator and no timezone — fails all 3 formats, but
	// len >= 10 so the fallback parses the first 10 chars successfully.
	got, ok := parseDateString("2026-01-15T10:20:30")
	if !ok {
		t.Fatalf("expected fallback parse to succeed")
	}
	if got.Year() != 2026 || got.Month() != 1 || got.Day() != 15 {
		t.Fatalf("unexpected date from fallback: %v", got)
	}
}

func TestSplitClientName_NoSpace(t *testing.T) {
	nachname, vorname := splitClientName("Madonna")
	if nachname != "Madonna" || vorname != "" {
		t.Fatalf("expected (Madonna, '') got (%q, %q)", nachname, vorname)
	}
}

// ── Raw export DB query error paths ──────────────────────────────────────────

func TestExportRawClientsCSV_QueryError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT").WillReturnError(errors.New("db down"))

	h := &Handler{DB: db}
	req := httptest.NewRequest(http.MethodGet, "/api/exports/raw/clients.csv", nil)
	w := httptest.NewRecorder()
	h.ExportRawClientsCSV(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestExportRawContractsCSV_QueryError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT").WillReturnError(errors.New("db down"))

	h := &Handler{DB: db}
	req := httptest.NewRequest(http.MethodGet, "/api/exports/raw/contracts.csv", nil)
	w := httptest.NewRecorder()
	h.ExportRawContractsCSV(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestExportRawCashflowEntriesCSV_QueryError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT").WillReturnError(errors.New("db down"))

	h := &Handler{DB: db}
	req := httptest.NewRequest(http.MethodGet, "/api/exports/raw/cashflow_entries.csv", nil)
	w := httptest.NewRecorder()
	h.ExportRawCashflowEntriesCSV(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

// ── ExportLegacyCashflowCSV DB error sub-paths ───────────────────────────────

func TestExportLegacyCashflowCSV_MainQueryError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT").WillReturnError(errors.New("db down"))

	h := &Handler{DB: db}
	req := httptest.NewRequest(http.MethodGet, "/api/exports/legacy/cashflow.csv", nil)
	w := httptest.NewRecorder()
	h.ExportLegacyCashflowCSV(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestExportLegacyCashflowCSV_CashflowQueryError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	// First query (clients+contracts) returns one row so we don't early-exit.
	clientRows := sqlmock.NewRows([]string{
		"id", "name", "status", "start_date", "end_date", "clv", "source", "source_stage_name",
	}).AddRow(1, "Müller Hans", "active", "2026-01-01", "2026-03-31", 300.0, "organic", "")
	mock.ExpectQuery("SELECT").WillReturnRows(clientRows)

	// Second query (cashflow entries) fails.
	mock.ExpectQuery("SELECT").WillReturnError(errors.New("cashflow db down"))

	h := &Handler{DB: db}
	req := httptest.NewRequest(http.MethodGet, "/api/exports/legacy/cashflow.csv", nil)
	w := httptest.NewRecorder()
	h.ExportLegacyCashflowCSV(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestExportLegacyCashflowCSV_UpsellQueryError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	clientRows := sqlmock.NewRows([]string{
		"id", "name", "status", "start_date", "end_date", "clv", "source", "source_stage_name",
	}).AddRow(1, "Müller Hans", "active", "2026-01-01", "2026-03-31", 300.0, "organic", "")
	mock.ExpectQuery("SELECT").WillReturnRows(clientRows)

	cashRows := sqlmock.NewRows([]string{"client_id", "ym", "amount"}) // empty
	mock.ExpectQuery("SELECT").WillReturnRows(cashRows)

	// Upsells query fails.
	mock.ExpectQuery("SELECT").WillReturnError(errors.New("upsell db down"))

	h := &Handler{DB: db}
	req := httptest.NewRequest(http.MethodGet, "/api/exports/legacy/cashflow.csv", nil)
	w := httptest.NewRecorder()
	h.ExportLegacyCashflowCSV(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestExportLegacyCashflowCSV_CommentQueryError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	clientRows := sqlmock.NewRows([]string{
		"id", "name", "status", "start_date", "end_date", "clv", "source", "source_stage_name",
	}).AddRow(1, "Müller Hans", "active", "2026-01-01", "2026-03-31", 300.0, "organic", "")
	mock.ExpectQuery("SELECT").WillReturnRows(clientRows)

	cashRows := sqlmock.NewRows([]string{"client_id", "ym", "amount"})
	mock.ExpectQuery("SELECT").WillReturnRows(cashRows)

	upsellRows := sqlmock.NewRows([]string{"client_id", "upsell_result"})
	mock.ExpectQuery("SELECT").WillReturnRows(upsellRows)

	// Comments query fails.
	mock.ExpectQuery("SELECT").WillReturnError(errors.New("comment db down"))

	h := &Handler{DB: db}
	req := httptest.NewRequest(http.MethodGet, "/api/exports/legacy/cashflow.csv", nil)
	w := httptest.NewRecorder()
	h.ExportLegacyCashflowCSV(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestExportLegacyCashflowCSV_NoUpsells(t *testing.T) {
	// Covers the 'keine verlängerung' default branch by having no upsell rows.
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	clientRows := sqlmock.NewRows([]string{
		"id", "name", "status", "start_date", "end_date", "clv", "source", "source_stage_name",
	}).AddRow(1, "Müller Hans", "active", "2026-01-01", "2026-03-31", 300.0, "organic", "")
	mock.ExpectQuery("SELECT").WillReturnRows(clientRows)

	cashRows := sqlmock.NewRows([]string{"client_id", "ym", "amount"})
	mock.ExpectQuery("SELECT").WillReturnRows(cashRows)

	upsellRows := sqlmock.NewRows([]string{"client_id", "upsell_result"})
	mock.ExpectQuery("SELECT").WillReturnRows(upsellRows)

	commentRows := sqlmock.NewRows([]string{"entity_id", "body"})
	mock.ExpectQuery("SELECT").WillReturnRows(commentRows)

	h := &Handler{DB: db}
	req := httptest.NewRequest(http.MethodGet, "/api/exports/legacy/cashflow.csv", nil)
	w := httptest.NewRecorder()
	h.ExportLegacyCashflowCSV(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "keine verlängerung") {
		t.Fatalf("expected keine verlängerung fallback, body=%s", w.Body.String())
	}
}
