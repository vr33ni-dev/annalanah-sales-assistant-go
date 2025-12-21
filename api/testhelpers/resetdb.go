package testhelpers

import "testing"

// ResetDB drops all user tables and re-runs migrations.
// Use this between integration tests to ensure a clean state.
func ResetDB(t *testing.T, db *TestDB) {
	t.Helper()

	_, err := db.DB.Exec(`
		DO $$
		DECLARE
			r RECORD;
		BEGIN
			FOR r IN (
				SELECT tablename
				FROM pg_tables
				WHERE schemaname = 'public'
				  AND tablename NOT LIKE 'pg_%'
				  AND tablename NOT LIKE 'sql_%'
			)
			LOOP
				EXECUTE 'DROP TABLE IF EXISTS "' || r.tablename || '" CASCADE';
			END LOOP;
		END $$;
	`)
	if err != nil {
		t.Fatalf("ResetDB: failed to drop tables: %v", err)
	}

	// Re-run migrations
	loadMigrations(db.DB, t)
}
