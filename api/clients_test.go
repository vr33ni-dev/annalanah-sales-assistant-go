package api_test

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3" // SQLite driver
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/api"
)

// createTestSchema creates all tables required by handlers that touch the DB.
func createTestSchema(t *testing.T, db *sql.DB) {
	schema := []string{
		`CREATE TABLE clients (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT,
			email TEXT,
			phone TEXT,
			source TEXT,
			source_stage_id INTEGER,
			status TEXT,
			completed_at DATETIME
		);`,
		`CREATE TABLE stages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT
		);`,
	}

	for _, stmt := range schema {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("failed to create schema: %v\nSQL: %s", err, stmt)
		}
	}
}

// --- Tests ---

func TestListClients(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	createTestSchema(t, db)

	// Seed a single client
	_, err = db.Exec(`
		INSERT INTO clients (name, email, phone, source, status)
		VALUES ('Alice', 'alice@example.com', '123', 'web', 'active')
	`)
	if err != nil {
		t.Fatal(err)
	}

	h := &api.Handler{DB: db}
	req := httptest.NewRequest(http.MethodGet, "/api/clients", nil)
	w := httptest.NewRecorder()

	h.ListClients(w, req)
	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}
}

func TestCreateClient(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	createTestSchema(t, db)

	h := &api.Handler{DB: db}
	body := strings.NewReader(`{
		"name": "Bob",
		"email": "bob@example.com",
		"phone": "456",
		"source": "referral",
		"status": "active"
	}`)

	req := httptest.NewRequest(http.MethodPost, "/api/clients", body)
	w := httptest.NewRecorder()

	h.CreateClient(w, req)
	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d", resp.StatusCode)
	}

	// Optional: verify client was inserted
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM clients WHERE name='Bob'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 client inserted, got %d", count)
	}
}

func TestDeleteClient(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	createTestSchema(t, db)

	// Seed one client
	res, err := db.Exec(`
		INSERT INTO clients (name, email, phone, source, status)
		VALUES ('Charlie', 'charlie@example.com', '789', 'ad', 'active')
	`)
	if err != nil {
		t.Fatal(err)
	}

	clientID, _ := res.LastInsertId()

	h := &api.Handler{DB: db}

	// Prepare DELETE request
	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/clients/%d", clientID), nil)
	w := httptest.NewRecorder()

	h.DeleteClient(w, req)
	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 No Content, got %d", resp.StatusCode)
	}

	// Verify the client is gone
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM clients WHERE id = ?`, clientID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected 0 clients after deletion, got %d", count)
	}
}
