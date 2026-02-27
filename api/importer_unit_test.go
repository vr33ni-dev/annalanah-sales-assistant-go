package api

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

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
