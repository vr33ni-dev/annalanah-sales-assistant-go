package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3" // SQLite driver
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
			completed_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE stages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT
		);`,
		`CREATE TABLE sales_process (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			client_id INTEGER,
			stage TEXT,
			closed BOOLEAN,
			initial_contact_date DATETIME,
			follow_up_date DATETIME,
			follow_up_result BOOLEAN
		);`,
		`CREATE TABLE contracts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			client_id INTEGER,
			end_date DATE
		);`,
		`CREATE TABLE leads (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT,
			email TEXT,
			phone TEXT,
			source TEXT,
			source_stage_id INTEGER,
			converted BOOLEAN DEFAULT FALSE,
			converted_at DATETIME,
			converted_client_id INTEGER
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

	h := &Handler{DB: db}
	req := httptest.NewRequest(http.MethodGet, "/api/clients", nil)
	w := httptest.NewRecorder()

	h.ListClients(w, req)
	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}
}

func TestListClients_ExpiredContractDoesNotStayActive(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	createTestSchema(t, db)

	_, err = db.Exec(`
		INSERT INTO clients (id, name, email, phone, source, status)
		VALUES (1, 'Expired Client', 'expired@example.com', '123', 'import', 'active')
	`)
	if err != nil {
		t.Fatal(err)
	}

	_, err = db.Exec(`
		INSERT INTO sales_process (client_id, stage, closed, initial_contact_date, follow_up_result)
		VALUES (1, 'closed', 1, ?, NULL)
	`, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	_, err = db.Exec(`
		INSERT INTO contracts (client_id, end_date)
		VALUES (1, ?)
	`, time.Now().AddDate(0, 0, -1).Format("2006-01-02"))
	if err != nil {
		t.Fatal(err)
	}

	h := &Handler{DB: db}
	req := httptest.NewRequest(http.MethodGet, "/api/clients?include_inactive=true", nil)
	w := httptest.NewRecorder()

	h.ListClients(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var out []map[string]any
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if len(out) != 1 {
		t.Fatalf("expected 1 client, got %d", len(out))
	}

	status, _ := out[0]["status"].(string)
	if status != "inactive" {
		t.Fatalf("expected expired-only client to be inactive, got %q", status)
	}
}

func TestListClients_ReturnsLeadIDWhenClientWasConvertedFromLead(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	createTestSchema(t, db)

	_, err = db.Exec(`
		INSERT INTO clients (id, name, email, phone, source, status)
		VALUES (1, 'Converted Client', 'converted@example.com', '123', 'web', 'active')
	`)
	if err != nil {
		t.Fatal(err)
	}

	_, err = db.Exec(`
		INSERT INTO leads (id, name, email, phone, source, converted, converted_at, converted_client_id)
		VALUES (10, 'Original Lead', 'lead@example.com', '456', 'web', 1, '2026-03-01 00:00:00', 1)
	`)
	if err != nil {
		t.Fatal(err)
	}

	h := &Handler{DB: db}
	req := httptest.NewRequest(http.MethodGet, "/api/clients?include_inactive=true", nil)
	w := httptest.NewRecorder()

	h.ListClients(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var out []map[string]any
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if len(out) != 1 {
		t.Fatalf("expected 1 client, got %d", len(out))
	}

	leadID, ok := out[0]["lead_id"].(float64)
	if !ok {
		t.Fatalf("expected lead_id in response, got %#v", out[0]["lead_id"])
	}
	if int(leadID) != 10 {
		t.Fatalf("expected lead_id 10, got %v", leadID)
	}
}

func TestListClients_ActiveContractKeepsClientActive(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	createTestSchema(t, db)

	_, err = db.Exec(`
		INSERT INTO clients (id, name, email, phone, source, status)
		VALUES (1, 'Mixed Client', 'mixed@example.com', '123', 'import', 'active')
	`)
	if err != nil {
		t.Fatal(err)
	}

	_, err = db.Exec(`
		INSERT INTO contracts (client_id, end_date)
		VALUES (1, ?), (1, ?)
	`, time.Now().AddDate(0, 0, -1).Format("2006-01-02"), time.Now().AddDate(0, 0, 5).Format("2006-01-02"))
	if err != nil {
		t.Fatal(err)
	}

	h := &Handler{DB: db}
	req := httptest.NewRequest(http.MethodGet, "/api/clients", nil)
	w := httptest.NewRecorder()

	h.ListClients(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var out []map[string]any
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if len(out) != 1 {
		t.Fatalf("expected 1 client, got %d", len(out))
	}

	status, _ := out[0]["status"].(string)
	if status != "active" {
		t.Fatalf("expected client with one active contract to be active, got %q", status)
	}
}

func TestCreateClient(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	createTestSchema(t, db)

	h := &Handler{DB: db}
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

func TestCreateClient_MissingStatus(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	createTestSchema(t, db)

	h := &Handler{DB: db}
	body := strings.NewReader(`{
		"name": "Bob",
		"email": "bob@example.com",
		"phone": "456",
		"source": "referral"
	}`)

	req := httptest.NewRequest(http.MethodPost, "/api/clients", body)
	w := httptest.NewRecorder()

	h.CreateClient(w, req)
	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d", resp.StatusCode)
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

	h := &Handler{DB: db}

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

func TestDeleteClient_ResetsLinkedLeadConversion(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	createTestSchema(t, db)

	res, err := db.Exec(`
		INSERT INTO clients (name, email, phone, source, status)
		VALUES ('Charlie', 'charlie@example.com', '789', 'ad', 'active')
	`)
	if err != nil {
		t.Fatal(err)
	}
	clientID, _ := res.LastInsertId()

	_, err = db.Exec(`
		INSERT INTO leads (name, email, phone, source, converted, converted_at, converted_client_id)
		VALUES ('Lead One', 'lead@example.com', '111', 'web', 1, CURRENT_TIMESTAMP, ?)
	`, clientID)
	if err != nil {
		t.Fatal(err)
	}

	h := &Handler{DB: db}
	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/clients/%d", clientID), nil)
	w := httptest.NewRecorder()

	h.DeleteClient(w, req)
	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 No Content, got %d", resp.StatusCode)
	}

	var converted bool
	var convertedAt sql.NullString
	var convertedClientID sql.NullInt64
	if err := db.QueryRow(`
		SELECT converted, converted_at, converted_client_id
		FROM leads
		WHERE email = 'lead@example.com'
	`).Scan(&converted, &convertedAt, &convertedClientID); err != nil {
		t.Fatal(err)
	}

	if converted {
		t.Fatal("expected lead to be reset to unconverted")
	}
	if convertedAt.Valid {
		t.Fatal("expected converted_at to be NULL after client deletion")
	}
	if convertedClientID.Valid {
		t.Fatal("expected converted_client_id to be NULL after client deletion")
	}
}

func TestListClients_DBError(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	h := &Handler{DB: db}

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
	h := &Handler{DB: db}

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
	h := &Handler{DB: db}

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
	h := &Handler{DB: db}

	body := strings.NewReader(`{"name":"Eve","status":"active"}`)
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
	h := &Handler{DB: db}

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
	h := &Handler{DB: db}

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
	h := &Handler{DB: db}

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
	h := &Handler{DB: db} // table missing = DB error

	body := strings.NewReader(`{"name":"Bob"}`)
	req := httptest.NewRequest(http.MethodPatch, "/api/clients/1", body)
	w := httptest.NewRecorder()
	h.UpdateClient(w, req)

	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500 update failed")
	}
}

func TestUpdateClient_CompletedAtBeforeCreatedAt(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	createTestSchema(t, db)
	_, err := db.Exec(`
		INSERT INTO clients (id, name, status, created_at)
		VALUES (1, 'Bob', 'active', '2026-03-01 12:00:00')
	`)
	if err != nil {
		t.Fatal(err)
	}

	h := &Handler{DB: db}
	req := httptest.NewRequest(http.MethodPatch, "/api/clients/1", strings.NewReader(`{"completed_at":"2026-02-01"}`))
	w := httptest.NewRecorder()

	h.UpdateClient(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "client creation") {
		t.Fatalf("expected creation-date validation message, got %q", w.Body.String())
	}
}

func TestUpdateClient_CompletedAtBeforeFollowUpDate(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	createTestSchema(t, db)
	_, err := db.Exec(`
		INSERT INTO clients (id, name, status, created_at)
		VALUES (1, 'Bob', 'active', '2026-01-01 12:00:00')
	`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		INSERT INTO sales_process (client_id, stage, closed, initial_contact_date, follow_up_date, follow_up_result)
		VALUES (1, 'follow_up', 0, '2026-01-10 00:00:00', '2026-02-15 00:00:00', NULL)
	`)
	if err != nil {
		t.Fatal(err)
	}

	h := &Handler{DB: db}
	req := httptest.NewRequest(http.MethodPatch, "/api/clients/1", strings.NewReader(`{"completed_at":"2026-02-01"}`))
	w := httptest.NewRecorder()

	h.UpdateClient(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "follow_up_date") {
		t.Fatalf("expected follow_up_date validation message, got %q", w.Body.String())
	}
}

func TestUpdateClient_CompletedAtValid(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	createTestSchema(t, db)
	_, err := db.Exec(`
		INSERT INTO clients (id, name, status, created_at)
		VALUES (1, 'Bob', 'active', '2026-01-01 12:00:00')
	`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		INSERT INTO sales_process (client_id, stage, closed, initial_contact_date, follow_up_date, follow_up_result)
		VALUES (1, 'follow_up', 0, '2026-01-10 00:00:00', '2026-02-01 00:00:00', NULL)
	`)
	if err != nil {
		t.Fatal(err)
	}

	h := &Handler{DB: db}
	req := httptest.NewRequest(http.MethodPatch, "/api/clients/1", strings.NewReader(`{"completed_at":"2026-02-15"}`))
	w := httptest.NewRecorder()

	h.UpdateClient(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	var completedAt sql.NullString
	if err := db.QueryRow(`SELECT completed_at FROM clients WHERE id = 1`).Scan(&completedAt); err != nil {
		t.Fatal(err)
	}
	if !completedAt.Valid || !strings.Contains(completedAt.String, "2026-02-15") {
		t.Fatalf("expected completed_at to be saved, got %#v", completedAt)
	}
}

func TestUpdateClient_SyncsEmailAndNameToLinkedLead(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	createTestSchema(t, db)

	// Seed client
	if _, err := db.Exec(`
		INSERT INTO clients (id, name, email, phone, source, status, created_at)
		VALUES (1, 'Old Name', 'old@email.com', '111', 'organic', 'active', '2026-01-01 12:00:00')
	`); err != nil {
		t.Fatal(err)
	}
	// Seed a converted lead pointing to client 1
	if _, err := db.Exec(`
		INSERT INTO leads (id, name, email, phone, source, converted, converted_client_id)
		VALUES (10, 'Old Name', 'old@email.com', '111', 'organic', 1, 1)
	`); err != nil {
		t.Fatal(err)
	}

	h := &Handler{DB: db}
	body := strings.NewReader(`{"name":"New Name","email":"new@email.com","phone":"999","source":"paid"}`)
	req := httptest.NewRequest(http.MethodPatch, "/api/clients/1", body)
	w := httptest.NewRecorder()
	h.UpdateClient(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	var leadName, leadEmail, leadPhone, leadSource string
	if err := db.QueryRow(`SELECT name, email, phone, source FROM leads WHERE id = 10`).
		Scan(&leadName, &leadEmail, &leadPhone, &leadSource); err != nil {
		t.Fatal(err)
	}
	if leadEmail != "new@email.com" {
		t.Errorf("expected lead email=new@email.com, got %q", leadEmail)
	}
	if leadName != "New Name" {
		t.Errorf("expected lead name=New Name, got %q", leadName)
	}
	if leadPhone != "999" {
		t.Errorf("expected lead phone=999, got %q", leadPhone)
	}
	if leadSource != "paid" {
		t.Errorf("expected lead source=paid, got %q", leadSource)
	}
}

func TestUpdateClient_InvalidCompletedAtFormat(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	createTestSchema(t, db)
	if _, err := db.Exec(`
		INSERT INTO clients (id, name, status, created_at)
		VALUES (1, 'Bob', 'active', '2026-01-01 12:00:00')
	`); err != nil {
		t.Fatal(err)
	}

	h := &Handler{DB: db}
	req := httptest.NewRequest(http.MethodPatch, "/api/clients/1", strings.NewReader(`{"completed_at":"not-a-date"}`))
	w := httptest.NewRecorder()
	h.UpdateClient(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "YYYY-MM-DD") {
		t.Fatalf("expected format hint in error, got %q", w.Body.String())
	}
}

func TestValidateClientCompletedAt(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	createTestSchema(t, db)

	h := &Handler{DB: db}
	// Insert client with created_at = 2025-01-01
	res, err := db.Exec(`INSERT INTO clients (name, status, created_at) VALUES ('A', 'active', '2025-01-01')`)
	if err != nil {
		t.Fatal(err)
	}
	clientID, _ := res.LastInsertId()

	// Case: completed_at before created_at
	badDate, _ := time.Parse("2006-01-02", "2024-12-31")
	err = h.validateClientCompletedAt(context.Background(), int(clientID), &badDate)
	if err == nil || !strings.Contains(err.Error(), "before client creation date") {
		t.Errorf("expected error for completed_at before created_at, got %v", err)
	}

	// Case: completed_at after created_at
	okDate, _ := time.Parse("2006-01-02", "2025-01-02")
	if err := h.validateClientCompletedAt(context.Background(), int(clientID), &okDate); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Insert sales_process with follow_up_date = 2025-01-10
	_, err = db.Exec(`INSERT INTO sales_process (client_id, follow_up_date) VALUES (?, '2025-01-10')`, clientID)
	if err != nil {
		t.Fatal(err)
	}
	// Case: completed_at before follow_up_date
	badFollowUp, _ := time.Parse("2006-01-02", "2025-01-05")
	err = h.validateClientCompletedAt(context.Background(), int(clientID), &badFollowUp)
	if err == nil || !strings.Contains(err.Error(), "before follow_up_date") {
		t.Errorf("expected error for completed_at before follow_up_date, got %v", err)
	}
}
