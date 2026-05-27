package testhelpers

import (
	"database/sql"
	"testing"
)

func MustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("SQL exec failed: %v\nSQL:\n%s", err, query)
	}
}

func MustQueryRow(t *testing.T, db *sql.DB, query string, args ...any) *sql.Row {
	t.Helper()
	return db.QueryRow(query, args...)
}
