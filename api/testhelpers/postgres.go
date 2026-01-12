package testhelpers

import (
	"context"
	"database/sql"
	"fmt"
	"net"
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

func SetupPostgres(t testing.TB) (*TestDB, error) {
	if t != nil {
		t.Helper()
	}

	ctx := context.Background()

	// Quick Docker availability check: if DOCKER_HOST points to a unix socket ensure it exists.
	dockerHost := os.Getenv("DOCKER_HOST")
	if dockerHost == "" {
		// try default docker socket
		dockerHost = "unix:///var/run/docker.sock"
	}
	if strings.HasPrefix(dockerHost, "unix://") {
		sock := strings.TrimPrefix(dockerHost, "unix://")
		if _, err := os.Stat(sock); err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("docker socket %s not found: %w", sock, err)
			}
			return nil, fmt.Errorf("cannot stat docker socket %s: %w", sock, err)
		}

		// try connecting to the socket to ensure Docker daemon is listening
		conn, err := net.DialTimeout("unix", sock, 500*time.Millisecond)
		if err != nil {
			return nil, fmt.Errorf("docker unix socket exists but not accepting connections %s: %w", sock, err)
		}
		_ = conn.Close()
	}

	container, err := postgres.RunContainer(
		ctx,
		tc.WithImage("postgres:14"), // explicit image
		postgres.WithDatabase("testdb"),
		postgres.WithUsername("testuser"),
		postgres.WithPassword("testpass"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to start postgres container: %w", err)
	}

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, err
	}

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, err
	}

	// Wait until Postgres is ready
	deadline := time.Now().Add(10 * time.Second)
	for {
		if err := db.Ping(); err == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = db.Close()
			_ = container.Terminate(ctx)
			return nil, fmt.Errorf("postgres did not become ready")
		}
		time.Sleep(100 * time.Millisecond)
	}

	loadMigrations(db, t)

	return &TestDB{
		DB:        db,
		container: container,
	}, nil
}

func (tdb *TestDB) TearDown(t testing.TB) {
	if tdb.DB != nil {
		tdb.DB.Close()
	}
	if tdb.container != nil {
		_ = tdb.container.Terminate(context.Background())
	}
}
