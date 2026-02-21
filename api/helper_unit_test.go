package api

import (
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
)

func TestGetNumericSetting_ReturnsValue(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to open sqlmock: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}

	mock.ExpectQuery(`SELECT value_numeric FROM app_settings WHERE key = \$1`).
		WithArgs("timeout").
		WillReturnRows(func() *sqlmock.Rows {
			r := sqlmock.NewRows([]string{"value_numeric"}).AddRow(2.5)
			return r
		}())

	got := h.getNumericSetting("timeout", 1.0)
	if got != 2.5 {
		t.Fatalf("expected 2.5, got %v", got)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestGetNumericSetting_DefaultOnNullOrMissing(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to open sqlmock: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}

	// return NULL
	mock.ExpectQuery(`SELECT value_numeric FROM app_settings WHERE key = \$1`).
		WithArgs("missing").
		WillReturnRows(func() *sqlmock.Rows {
			r := sqlmock.NewRows([]string{"value_numeric"}).AddRow(nil)
			return r
		}())

	got := h.getNumericSetting("missing", 7.2)
	if got != 7.2 {
		t.Fatalf("expected default 7.2, got %v", got)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestIsUniqueViolation(t *testing.T) {
	// matching pq error
	pe := &pq.Error{Code: "23505", Constraint: "unique_client_email"}
	if !isUniqueViolation(pe, "unique_client_email") {
		t.Fatalf("expected true for matching pq error")
	}

	// wrong constraint
	if isUniqueViolation(pe, "other") {
		t.Fatalf("expected false for non-matching constraint")
	}

	// non-pq error
	if isUniqueViolation(errors.New("boom"), "unique_client_email") {
		t.Fatalf("expected false for non-pq error")
	}
}
