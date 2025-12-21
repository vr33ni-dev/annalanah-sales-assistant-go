package testhelpers

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"
	tc "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

type TestDB struct {
	DB        *sql.DB
	container *postgres.PostgresContainer
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
	if t != nil {
		t.Helper()
	}

	ctx := context.Background()

	container, err := postgres.RunContainer(
		ctx,
		tc.WithImage("postgres:14"), // ✅ THIS is the fix
		postgres.WithDatabase("testdb"),
		postgres.WithUsername("testuser"),
		postgres.WithPassword("testpass"),
	)
	if err != nil {
		panic(fmt.Sprintf("failed to start postgres container: %v", err))
	}

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		panic(err)
	}

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		panic(err)
	}

	// Wait until Postgres is ready
	deadline := time.Now().Add(10 * time.Second)
	for {
		if err := db.Ping(); err == nil {
			break
		}
		if time.Now().After(deadline) {
			panic("postgres did not become ready")
		}
		time.Sleep(100 * time.Millisecond)
	}

	loadMigrations(db, t)

	return &TestDB{
		DB:        db,
		container: container,
	}
}

func (tdb *TestDB) TearDown(t testing.TB) {
	if tdb.DB != nil {
		tdb.DB.Close()
	}
	if tdb.container != nil {
		_ = tdb.container.Terminate(context.Background())
	}
}
