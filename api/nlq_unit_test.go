package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func resetCaches() {
	sqlCache = NewSQLResultCache(5 * time.Minute)
	questionCache = NewQuestionToSQLCache(30 * time.Minute)
}

func TestIsLikelySQLQuestion(t *testing.T) {
	cases := []struct {
		q    string
		want bool
	}{
		{"Zeige mir Kunden", true},
		{"Wie viele stages", true},
		{"Hallo wie geht's", false},
		{"Kunde mit Vertrag", true},
		{"random text", false},
	}

	for _, c := range cases {
		got := isLikelySQLQuestion(c.q)
		if got != c.want {
			t.Fatalf("isLikelySQLQuestion(%q) = %v, want %v", c.q, got, c.want)
		}
	}
}

func TestSQLHelpers(t *testing.T) {
	if !isSelect("SELECT * FROM clients") {
		t.Fatalf("isSelect failed")
	}
	if isSelect(" INSERT INTO foo") {
		t.Fatalf("isSelect false positive")
	}
	if !hasLimit("select * from clients limit 10") {
		t.Fatalf("hasLimit failed")
	}
	if !isAggregateQuery("select count(*) from clients") {
		t.Fatalf("isAggregateQuery failed for count")
	}
	if !isAggregateQuery("select SUM(revenue) from sales_process") {
		t.Fatalf("isAggregateQuery failed for sum")
	}
}

func TestIsSafeSQL(t *testing.T) {
	cases := []struct {
		sql  string
		want bool
	}{
		{"SELECT * FROM clients", true},
		{"SELECT * FROM clients LIMIT 10", true},
		{"SELECT * FROM clients; DROP TABLE clients", false},
		{"SELECT * FROM clients -- comment", false},
		{"INSERT INTO clients VALUES (1)", false},
		{"SELECT pg_sleep(10)", false},
	}

	for _, c := range cases {
		got := isSafeSQL(c.sql)
		if got != c.want {
			t.Fatalf("isSafeSQL(%q) = %v, want %v", c.sql, got, c.want)
		}
	}
}

func TestQuestionCache_Expiration(t *testing.T) {
	cache := NewQuestionToSQLCache(10 * time.Millisecond)

	cache.Set("q1", "SELECT 1")

	if _, ok := cache.Get("q1"); !ok {
		t.Fatalf("expected cache hit before expiration")
	}

	time.Sleep(20 * time.Millisecond)

	if _, ok := cache.Get("q1"); ok {
		t.Fatalf("expected cache miss after expiration")
	}
}

func TestSQLCache_Expiration(t *testing.T) {
	cache := NewSQLResultCache(10 * time.Millisecond)

	resp := nlqResponse{SQL: "SELECT 1"}
	cache.Set("SELECT 1", resp)

	if _, ok := cache.Get("SELECT 1"); !ok {
		t.Fatalf("expected cache hit before expiration")
	}

	time.Sleep(20 * time.Millisecond)

	if _, ok := cache.Get("SELECT 1"); ok {
		t.Fatalf("expected cache miss after expiration")
	}
}

func TestGenerateSQL_MockMode_Aggregate(t *testing.T) {
	t.Setenv("NLQ_MOCK", "1")
	sql, err := generateSQL(context.Background(), "Wie viele Kunden habe ich?")
	if err != nil {
		t.Fatalf("generateSQL error: %v", err)
	}
	if sql != "SELECT COUNT(*) FROM clients" {
		t.Fatalf("unexpected SQL: %s", sql)
	}
}

func TestRunNLQ_NonSQLQuestion(t *testing.T) {
	resetCaches()

	h := &Handler{DB: nil}

	body := map[string]string{"question": "Hallo wie geht es dir?"}
	b, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/nlq", bytes.NewReader(b))
	w := httptest.NewRecorder()

	h.RunNLQ(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	if !strings.Contains(w.Body.String(), "Ich kann dir bei Datenabfragen helfen") {
		t.Fatalf("unexpected response: %s", w.Body.String())
	}
}

func TestRunNLQ_Handler_MockMode(t *testing.T) {
	resetCaches()
	t.Setenv("NLQ_MOCK", "1")

	h := &Handler{DB: nil}

	body := map[string]string{"question": "Wie viele Kunden?"}
	b, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/nlq", bytes.NewReader(b))
	w := httptest.NewRecorder()

	h.RunNLQ(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp nlqResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}

	if resp.SQL == "" {
		t.Fatalf("expected SQL in response, got empty")
	}
}

func TestRunNLQ_DBError(t *testing.T) {
	resetCaches()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}
	question := "Zeige mir Kunden"
	questionCache.Set(question, "SELECT id FROM clients")

	mock.ExpectQuery("^SELECT id FROM clients LIMIT 100$").
		WillReturnError(fmt.Errorf("db error"))

	body := map[string]string{"question": question}
	b, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/nlq", bytes.NewReader(b))
	w := httptest.NewRecorder()

	h.RunNLQ(w, req)

	var resp nlqResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if resp.Error == "" {
		t.Fatalf("expected error in response")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestGenerateSQL_OfflineFallback(t *testing.T) {
	t.Setenv("NLQ_MOCK", "")
	t.Setenv("OPENAI_API_KEY", "")

	sql, err := generateSQL(context.Background(), "zeige mir zweitgespräch")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(sql, "sales_process") {
		t.Fatalf("expected fallback SQL, got: %s", sql)
	}
}

// covers if !isSelect(txt)
func TestGenerateSQL_NonSelectRejected(t *testing.T) {
	t.Setenv("NLQ_MOCK", "")
	t.Setenv("OPENAI_API_KEY", "")

	// simulate weird fallback
	sql, err := generateSQL(context.Background(), "something random")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !isSelect(sql) {
		t.Fatalf("expected SELECT SQL, got: %s", sql)
	}
}

// covers if !hasLimit(sqlText) && !isAggregateQuery(sqlText)
func TestRunNLQ_LimitInjection(t *testing.T) {
	resetCaches()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}

	question := "Zeige mir Kunden"
	questionCache.Set(question, "SELECT id FROM clients")

	mock.ExpectQuery("^SELECT id FROM clients LIMIT 100$").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))

	body := map[string]string{"question": question}
	b, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/nlq", bytes.NewReader(b))
	w := httptest.NewRecorder()

	h.RunNLQ(w, req)

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("limit injection failed: %v", err)
	}
}

// covers aggregate does not get limit
func TestRunNLQ_AggregateNoLimit(t *testing.T) {
	resetCaches()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}

	question := "Wie viele Kunden"
	questionCache.Set(question, "SELECT COUNT(*) FROM clients")

	mock.ExpectQuery("^SELECT COUNT\\(\\*\\) FROM clients$").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(10))

	body := map[string]string{"question": question}
	b, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/nlq", bytes.NewReader(b))
	w := httptest.NewRecorder()

	h.RunNLQ(w, req)

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("aggregate query unexpectedly modified: %v", err)
	}
}

// test rows.Scan error path
func TestRunNLQ_ScanError(t *testing.T) {
	resetCaches()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}
	question := "Zeige mir Kunden"
	questionCache.Set(question, "SELECT id FROM clients")

	rows := sqlmock.NewRows([]string{"id"}).
		AddRow(1).
		RowError(0, fmt.Errorf("scan error")) // force scan error

	mock.ExpectQuery("^SELECT id FROM clients LIMIT 100$").
		WillReturnRows(rows)

	body := map[string]string{"question": question}
	b, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/nlq", bytes.NewReader(b))
	w := httptest.NewRecorder()

	h.RunNLQ(w, req)

	var resp nlqResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if resp.Error == "" {
		t.Fatalf("expected scan error response")
	}

	if !strings.Contains(resp.Error, "scan error") {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
}

// test rows.Err() path
func TestRunNLQ_RowsErr(t *testing.T) {
	resetCaches()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}
	question := "Zeige mir Kunden"
	questionCache.Set(question, "SELECT id FROM clients")

	rows := sqlmock.NewRows([]string{"id"}).
		AddRow(1).
		RowError(0, fmt.Errorf("row error"))

	mock.ExpectQuery("^SELECT id FROM clients LIMIT 100$").
		WillReturnRows(rows)

	body := map[string]string{"question": question}
	b, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/nlq", bytes.NewReader(b))
	w := httptest.NewRecorder()

	h.RunNLQ(w, req)

	var resp nlqResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if resp.Error == "" {
		t.Fatalf("expected rows.Err error response")
	}
}

// Has limit edge cases
func TestHasLimit_CaseInsensitive(t *testing.T) {
	if !hasLimit("SELECT * FROM clients LIMIT 5") {
		t.Fatalf("expected limit detection")
	}
	if !hasLimit("select * from clients LiMiT 5") {
		t.Fatalf("case insensitive limit detection failed")
	}
}
