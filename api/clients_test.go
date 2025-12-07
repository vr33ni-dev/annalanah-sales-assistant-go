package api_test

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

func TestListClients_DBError(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	h := &api.Handler{DB: db}

	// Drop table so query fails
	req := httptest.NewRequest(http.MethodGet, "/api/clients", nil)
	w := httptest.NewRecorder()

	h.ListClients(w, req)

	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Result().StatusCode)
	}
}

func TestListClients_ScanError(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	db.Exec(`CREATE TABLE clients (id INT, name TEXT);`)
	db.Exec(`INSERT INTO clients (id, name) VALUES (1, 'bad')`)
	h := &api.Handler{DB: db}

	req := httptest.NewRequest(http.MethodGet, "/api/clients", nil)
	w := httptest.NewRecorder()
	h.ListClients(w, req)

	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500 on scan error, got %d", w.Result().StatusCode)
	}
}

func TestCreateClient_BadJSON(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	h := &api.Handler{DB: db}

	req := httptest.NewRequest(http.MethodPost, "/api/clients", strings.NewReader("{bad json"))
	w := httptest.NewRecorder()
	h.CreateClient(w, req)

	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid JSON")
	}
}

func TestCreateClient_DBError(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close() // closed → error
	h := &api.Handler{DB: db}

	body := strings.NewReader(`{"name":"Eve"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/clients", body)
	w := httptest.NewRecorder()
	h.CreateClient(w, req)

	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500 for DB error")
	}
}

func TestDeleteClient_InvalidID(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	h := &api.Handler{DB: db}

	req := httptest.NewRequest(http.MethodDelete, "/api/clients/abc", nil)
	w := httptest.NewRecorder()
	h.DeleteClient(w, req)

	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 invalid ID")
	}
}

func TestDeleteClient_NotFound(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	createTestSchema(t, db)
	h := &api.Handler{DB: db}

	req := httptest.NewRequest(http.MethodDelete, "/api/clients/99", nil)
	w := httptest.NewRecorder()
	h.DeleteClient(w, req)

	if w.Result().StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 not found, got %d", w.Result().StatusCode)
	}
}

func TestUpdateClient_Errors(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	createTestSchema(t, db)
	h := &api.Handler{DB: db}

	cases := []struct {
		name string
		path string
		body string
		want int
	}{
		{"invalid id", "/api/clients/x", `{"name":"X"}`, http.StatusBadRequest},
		{"empty body", "/api/clients/1", ``, http.StatusBadRequest},
		{"bad json", "/api/clients/1", `{invalid}`, http.StatusBadRequest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPatch, tc.path, strings.NewReader(tc.body))
			w := httptest.NewRecorder()
			h.UpdateClient(w, req)
			if w.Result().StatusCode != tc.want {
				t.Fatalf("got %d, want %d", w.Result().StatusCode, tc.want)
			}
		})
	}
}

func TestUpdateClient_DBError(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	h := &api.Handler{DB: db} // table missing = DB error

	body := strings.NewReader(`{"name":"Bob"}`)
	req := httptest.NewRequest(http.MethodPatch, "/api/clients/1", body)
	w := httptest.NewRecorder()
	h.UpdateClient(w, req)

	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500 update failed")
	}
}

func TestNullHelpers(t *testing.T) {
	if s := api.NullStrForTest(""); s.Valid {
		t.Fatal("empty string should be invalid")
	}
	i := 5
	if !api.NullIntForTest(&i).Valid {
		t.Fatal("int should be valid")
	}
	now := time.Now()
	if !api.NullTimeForTest(&now).Valid {
		t.Fatal("time should be valid")
	}
}

func TestParseIDFromURL(t *testing.T) {
	if _, ok := api.ParseIDFromURLForTest("/api/clients/42"); !ok {
		t.Fatal("expected valid ID")
	}
	if _, ok := api.ParseIDFromURLForTest("/api/clients/"); ok {
		t.Fatal("expected invalid")
	}
}

func TestWriteJSONError(t *testing.T) {
	w := httptest.NewRecorder()
	api.WriteJSONErrorForTest(w, "oops", 400)
	if w.Result().StatusCode != 400 {
		t.Fatal("expected 400")
	}
}
