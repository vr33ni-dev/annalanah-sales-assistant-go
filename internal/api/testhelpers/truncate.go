package testhelpers

import "testing"

func TruncateAll(t *testing.T, db *TestDB) {
	t.Helper()

	_, err := db.DB.Exec(`
		TRUNCATE
			cashflow_entries,
			contracts,
			sales_process,
			clients
		RESTART IDENTITY CASCADE
	`)
	if err != nil {
		t.Fatalf("TruncateAll failed: %v", err)
	}
}
