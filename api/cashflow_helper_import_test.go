package api

import (
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestInsertImportedCashflowEntriesTx_FloatValue_InsertsCashflowEntry(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("db.Begin: %v", err)
	}

	dueDate, _ := time.Parse("2006-01", "2025-03")
	mock.ExpectExec("INSERT INTO cashflow_entries").
		WithArgs(7, dueDate, 250.0).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectRollback()

	err = insertImportedCashflowEntriesTx(tx, 7, 1, map[string]any{
		"2025-03": 250.0,
	})
	if err != nil {
		t.Fatalf("insertImportedCashflowEntriesTx failed: %v", err)
	}

	if err := tx.Rollback(); err != nil {
		t.Fatalf("tx.Rollback: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestInsertImportedCashflowEntriesTx_MixedString_InsertsCashflowAndComment(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("db.Begin: %v", err)
	}

	dueDate, _ := time.Parse("2006-01", "2025-04")
	mock.ExpectExec("INSERT INTO cashflow_entries").
		WithArgs(9, dueDate, 190.0).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO comments").
		WithArgs(3, "2025-04: ? 190").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectRollback()

	err = insertImportedCashflowEntriesTx(tx, 9, 3, map[string]any{
		"2025-04": "? 190",
	})
	if err != nil {
		t.Fatalf("insertImportedCashflowEntriesTx failed: %v", err)
	}

	if err := tx.Rollback(); err != nil {
		t.Fatalf("tx.Rollback: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestInsertImportedCashflowEntriesTx_TextOnly_InsertsCommentOnly(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("db.Begin: %v", err)
	}

	mock.ExpectExec("INSERT INTO comments").
		WithArgs(5, "2025-05: delayed invoice").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectRollback()

	err = insertImportedCashflowEntriesTx(tx, 11, 5, map[string]any{
		"2025-05": "delayed invoice",
	})
	if err != nil {
		t.Fatalf("insertImportedCashflowEntriesTx failed: %v", err)
	}

	if err := tx.Rollback(); err != nil {
		t.Fatalf("tx.Rollback: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestInsertImportedCashflowEntriesTx_PlaceholderAndInvalidDate_NoWrites(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("db.Begin: %v", err)
	}
	mock.ExpectRollback()

	err = insertImportedCashflowEntriesTx(tx, 12, 6, map[string]any{
		"invalid": "text",
		"2025-06": "-",
	})
	if err != nil {
		t.Fatalf("insertImportedCashflowEntriesTx failed: %v", err)
	}

	if err := tx.Rollback(); err != nil {
		t.Fatalf("tx.Rollback: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestInsertImportedCashflowEntriesTx_ReturnsErrorOnCashflowInsertFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("db.Begin: %v", err)
	}

	dueDate, _ := time.Parse("2006-01", "2025-07")
	mock.ExpectExec("INSERT INTO cashflow_entries").
		WithArgs(21, dueDate, 99000.0).
		WillReturnError(errors.New("insert failed"))
	mock.ExpectRollback()

	err = insertImportedCashflowEntriesTx(tx, 21, 8, map[string]any{
		"2025-07": 99.0,
	})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if err.Error() != "insert failed" {
		t.Fatalf("expected insert failed error, got %v", err)
	}

	if err := tx.Rollback(); err != nil {
		t.Fatalf("tx.Rollback: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestInsertImportedCashflowEntriesTx_ZeroFloat_Skipped(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	tx, _ := db.Begin()
	mock.ExpectRollback()

	// Zero float value must be skipped — no DB writes expected.
	err = insertImportedCashflowEntriesTx(tx, 30, 10, map[string]any{
		"2025-08": 0.0,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tx.Rollback()

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestInsertImportedCashflowEntriesTx_EmptyString_Skipped(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	tx, _ := db.Begin()
	mock.ExpectRollback()

	// Empty string must be skipped — no DB writes.
	err = insertImportedCashflowEntriesTx(tx, 31, 11, map[string]any{
		"2025-09": "   ",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tx.Rollback()

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestInsertImportedCashflowEntriesTx_StringZeroNumber_Skipped(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	tx, _ := db.Begin()
	mock.ExpectRollback()

	// A string whose extracted number is 0 must be skipped.
	err = insertImportedCashflowEntriesTx(tx, 32, 12, map[string]any{
		"2025-10": "0",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tx.Rollback()

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestInsertImportedCashflowEntriesTx_CommentInsertError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	tx, _ := db.Begin()
	mock.ExpectExec("INSERT INTO comments").
		WithArgs(20, "2025-11: some note").
		WillReturnError(errors.New("comment insert failed"))
	mock.ExpectRollback()

	err = insertImportedCashflowEntriesTx(tx, 40, 20, map[string]any{
		"2025-11": "some note",
	})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	tx.Rollback()

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
