package testhelpers

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	_ "github.com/lib/pq"
)

/* Start database & load schema */
type TestDB struct {
	DB       *sql.DB
	postgres *embeddedpostgres.EmbeddedPostgres
}

func findProjectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "db", "migrations")); err == nil {
			return dir, nil
		}

		next := filepath.Dir(dir)
		if next == dir {
			return "", fmt.Errorf("could not find project root containing db/migrations")
		}
		dir = next
	}
}

func loadMigrations(db *sql.DB, t testing.TB) {
	root, err := findProjectRoot()
	if err != nil {
		t.Fatalf("%v", err)
	}

	mDir := filepath.Join(root, "db", "migrations")

	files, err := os.ReadDir(mDir)
	if err != nil {
		t.Fatalf("could not read migrations dir at %s: %v", mDir, err)
	}

	for _, f := range files {
		// Only run upward migrations
		if !strings.HasSuffix(f.Name(), ".up.sql") {
			continue
		}

		content, err := os.ReadFile(filepath.Join(mDir, f.Name()))
		if err != nil {
			t.Fatalf("could not read migration file %s: %v", f.Name(), err)
		}

		if _, err := db.Exec(string(content)); err != nil {
			t.Fatalf("migration %s failed: %v", f.Name(), err)
		}
	}
}

func SetupPostgres(t testing.TB) *TestDB {
	t.Helper()

	pg := embeddedpostgres.NewDatabase(
		embeddedpostgres.DefaultConfig().
			Port(15432).
			Database("testdb").
			Username("testuser").
			Password("testpass"),
	)

	if err := pg.Start(); err != nil {
		t.Fatalf("failed to start embedded postgres: %v", err)
	}

	conn := "postgres://testuser:testpass@localhost:15432/testdb?sslmode=disable"

	db, err := sql.Open("postgres", conn)
	if err != nil {
		t.Fatalf("failed to open DB: %v", err)
	}

	if err := db.Ping(); err != nil {
		t.Fatalf("failed to ping DB: %v", err)
	}

	loadMigrations(db, t)

	return &TestDB{DB: db, postgres: pg}
}

func (tdb *TestDB) TearDown(t testing.TB) {
	t.Helper()
	tdb.DB.Close()
	tdb.postgres.Stop()
}
