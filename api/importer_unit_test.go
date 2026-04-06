package api

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestImportContracts_BlockInProduction(t *testing.T) {
	os.Setenv("APP_ENV", "production")
	// set MIGRATION_KEY so the handler expects a header but we omit it
	os.Setenv("MIGRATION_KEY", "ALLOW_MIGRATION")
	defer os.Unsetenv("MIGRATION_KEY")
	defer os.Unsetenv("APP_ENV")

	h := &Handler{DB: nil}

	// valid empty payload must be accepted by JSON decoder
	req := httptest.NewRequest(http.MethodPost, "/api/import/contracts", bytes.NewReader([]byte("[]")))
	w := httptest.NewRecorder()

	h.ImportContracts(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when APP_ENV=production, got %d: %s", w.Code, w.Body.String())
	}
}

func TestImportContracts_MissingMigrationKey(t *testing.T) {
	os.Unsetenv("APP_ENV")

	h := &Handler{DB: nil}

	req := httptest.NewRequest(http.MethodPost, "/api/import/contracts", bytes.NewReader([]byte("[]")))
	w := httptest.NewRecorder()

	h.ImportContracts(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when X-Migration-Key missing, got %d: %s", w.Code, w.Body.String())
	}
}

func TestImportContracts_TruncateFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	// Expect TRUNCATE call and simulate error
	mock.ExpectExec("TRUNCATE TABLE").WillReturnError(errors.New("boom"))

	h := &Handler{DB: db}

	// ensure migration key env matches header check
	os.Setenv("MIGRATION_KEY", "ALLOW_MIGRATION")
	defer os.Unsetenv("MIGRATION_KEY")

	req := httptest.NewRequest(http.MethodPost, "/api/import/contracts", bytes.NewReader([]byte("[]")))
	req.Header.Set("X-Migration-Key", "ALLOW_MIGRATION")
	w := httptest.NewRecorder()

	h.ImportContracts(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when truncate fails, got %d: %s", w.Code, w.Body.String())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestImportContracts_CreatesPlaceholderSalesProcess(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}

	payload := `[
		{
			"name": "Imported Client",
			"contract_start": "2025-01-01",
			"contract_end": "2025-04-01",
			"cashflows": {}
		}
	]`

	mock.ExpectExec("TRUNCATE TABLE").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO clients").
		WithArgs("Imported Client", nil, "active", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(11))
	mock.ExpectQuery("INSERT INTO sales_process").
		WithArgs(11, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(21))
	mock.ExpectQuery("INSERT INTO contracts").
		WithArgs(
			11,
			21,
			time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2025, 4, 1, 0, 0, 0, 0, time.UTC),
			3,
			0.0,
			"monthly",
			"imported",
			time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(31, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)))
	mock.ExpectExec("INSERT INTO contract_upsells").
		WithArgs(21, 11, 0.0, 31).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	req := httptest.NewRequest(http.MethodPost, "/api/import/contracts", bytes.NewReader([]byte(payload)))
	req.Header.Set("X-Migration-Key", "ALLOW_MIGRATION")
	w := httptest.NewRecorder()

	h.ImportContracts(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestDetectPaymentFreq(t *testing.T) {
	cases := []struct {
		name      string
		cashflows map[string]interface{}
		want      string
	}{
		{
			name:      "empty cashflows → monthly",
			cashflows: map[string]interface{}{},
			want:      "monthly",
		},
		{
			name:      "single payment → one-time",
			cashflows: map[string]interface{}{"2025-03": 500.0},
			want:      "one-time",
		},
		{
			name: "diff >= 6 → bi-yearly",
			cashflows: map[string]interface{}{
				"2025-01": 500.0,
				"2025-07": 500.0,
			},
			want: "bi-yearly",
		},
		{
			name: "diff >= 3 → quarterly",
			cashflows: map[string]interface{}{
				"2025-01": 500.0,
				"2025-04": 500.0,
			},
			want: "quarterly",
		},
		{
			name: "diff >= 2 → bi-monthly",
			cashflows: map[string]interface{}{
				"2025-01": 500.0,
				"2025-03": 500.0,
			},
			want: "bi-monthly",
		},
		{
			name: "diff 1 → monthly",
			cashflows: map[string]interface{}{
				"2025-01": 500.0,
				"2025-02": 500.0,
			},
			want: "monthly",
		},
		{
			name: "zero values ignored → one-time (only one positive)",
			cashflows: map[string]interface{}{
				"2025-01": 0.0,
				"2025-02": 500.0,
			},
			want: "one-time",
		},
		{
			name: "invalid date key ignored",
			cashflows: map[string]interface{}{
				"not-a-date": 100.0,
				"2025-06":    100.0,
			},
			want: "one-time",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := detectPaymentFreq(tc.cashflows)
			if got != tc.want {
				t.Errorf("detectPaymentFreq() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseISO(t *testing.T) {
	cases := []struct {
		input   string
		wantErr bool
		wantDay int
	}{
		{"2025-01-15", false, 15},
		{"2025-01-15T10:30:00", false, 15},
		{"2025-01-15T10:30:00Z", false, 15},
		{"", true, 0},
		{"not-a-date", true, 0},
		{"01/15/2025", true, 0},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := parseISO(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("parseISO(%q) error = %v, wantErr = %v", tc.input, err, tc.wantErr)
			}
			if !tc.wantErr && got.Day() != tc.wantDay {
				t.Errorf("parseISO(%q).Day() = %d, want %d", tc.input, got.Day(), tc.wantDay)
			}
		})
	}
}

func TestParseCLV(t *testing.T) {
	cases := []struct {
		input string
		want  float64
	}{
		// kEUR format: value < 100 is multiplied by 1000
		{"€7.20", 7200},
		{"€9.75", 9750},
		{"€4.50", 4500},
		{"€1.80", 1800},
		// already in EUR: value >= 100 used as-is
		{"€900.00", 900},
		{"€1200.00", 1200},
		{"€150.00", 150},
		// comma as decimal separator (European)
		{"€7,20", 7200},
		{"€900,00", 900},
		// no currency symbol
		{"7.20", 7200},
		{"900.00", 900},
		// whitespace
		{" €7.20 ", 7200},
		// empty / invalid → 0
		{"", 0},
		{"abc", 0},
	}

	for _, tc := range cases {
		got := parseCLV(tc.input)
		if got != tc.want {
			t.Errorf("parseCLV(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestImportContracts_EmailFieldPropagated(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}

	payload := `[
		{
			"name": "Email Client",
			"email": "test@example.com",
			"contract_start": "2025-01-01",
			"contract_end": "2025-07-01",
			"cashflows": {}
		}
	]`

	os.Setenv("MIGRATION_KEY", "ALLOW_MIGRATION")
	defer os.Unsetenv("MIGRATION_KEY")

	mock.ExpectExec("TRUNCATE TABLE").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectBegin()
	// INSERT INTO clients must receive the email value
	mock.ExpectQuery("INSERT INTO clients").
		WithArgs("Email Client", "test@example.com", "active", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(50))
	mock.ExpectQuery("INSERT INTO sales_process").
		WithArgs(50, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(60))
	mock.ExpectQuery("INSERT INTO contracts").
		WithArgs(
			50,
			60,
			time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC),
			6,
			0.0,
			"monthly",
			"imported",
			time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(70, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)))
	mock.ExpectExec("INSERT INTO contract_upsells").
		WithArgs(60, 50, 0.0, 70).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	req := httptest.NewRequest(http.MethodPost, "/api/import/contracts", bytes.NewReader([]byte(payload)))
	req.Header.Set("X-Migration-Key", "ALLOW_MIGRATION")
	w := httptest.NewRecorder()

	h.ImportContracts(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestImportContracts_InvalidStartDate_Returns400(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}

	payload := `[{"name":"Bad Start","contract_start":"not-a-date","contract_end":"2025-07-01","cashflows":{}}]`

	mock.ExpectExec("TRUNCATE TABLE").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectBegin()
	mock.ExpectRollback()

	req := httptest.NewRequest(http.MethodPost, "/api/import/contracts", bytes.NewReader([]byte(payload)))
	req.Header.Set("X-Migration-Key", "ALLOW_MIGRATION")
	w := httptest.NewRecorder()

	h.ImportContracts(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestImportContracts_InvalidEndDate_Returns400(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}

	payload := `[{"name":"Bad End","contract_start":"2025-01-01","contract_end":"not-a-date","cashflows":{}}]`

	mock.ExpectExec("TRUNCATE TABLE").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectBegin()
	mock.ExpectRollback()

	req := httptest.NewRequest(http.MethodPost, "/api/import/contracts", bytes.NewReader([]byte(payload)))
	req.Header.Set("X-Migration-Key", "ALLOW_MIGRATION")
	w := httptest.NewRecorder()

	h.ImportContracts(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestImportContracts_EndBeforeStart_Returns400(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}

	payload := `[{"name":"Reversed Dates","contract_start":"2025-07-01","contract_end":"2025-01-01","cashflows":{}}]`

	mock.ExpectExec("TRUNCATE TABLE").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectBegin()
	mock.ExpectRollback()

	req := httptest.NewRequest(http.MethodPost, "/api/import/contracts", bytes.NewReader([]byte(payload)))
	req.Header.Set("X-Migration-Key", "ALLOW_MIGRATION")
	w := httptest.NewRecorder()

	h.ImportContracts(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestImportContracts_FormerClientInvalidDates_Skipped(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}

	// Three former clients with bad dates — all skipped, result is 200 with skipped list.
	payload := `[
		{"name":"Former Bad Start","is_former":true,"contract_start":"not-a-date","contract_end":"2025-07-01","cashflows":{}},
		{"name":"Former Bad End","is_former":true,"contract_start":"2025-01-01","contract_end":"not-a-date","cashflows":{}},
		{"name":"Former Reversed","is_former":true,"contract_start":"2025-07-01","contract_end":"2025-01-01","cashflows":{}}
	]`

	mock.ExpectExec("TRUNCATE TABLE").WillReturnResult(sqlmock.NewResult(0, 0))
	// Each former client opens a tx then rolls back on parse failure.
	mock.ExpectBegin()
	mock.ExpectRollback()
	mock.ExpectBegin()
	mock.ExpectRollback()
	mock.ExpectBegin()
	mock.ExpectRollback()

	req := httptest.NewRequest(http.MethodPost, "/api/import/contracts", bytes.NewReader([]byte(payload)))
	req.Header.Set("X-Migration-Key", "ALLOW_MIGRATION")
	w := httptest.NewRecorder()

	h.ImportContracts(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
