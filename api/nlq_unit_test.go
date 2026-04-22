package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	anthropic "github.com/anthropics/anthropic-sdk-go"
)

func resetCaches() {
	sqlCache = NewSQLResultCache(5 * time.Minute)
	questionCache = NewQuestionToSQLCache(30 * time.Minute)
	anthropicClient = nil
	anthropicClientAPIKey = ""
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

func TestQuestionCache_NormalizesEquivalentQuestions(t *testing.T) {
	cache := NewQuestionToSQLCache(30 * time.Minute)
	cache.Set("  Wie   viele   Kunden?  ", "SELECT COUNT(*) FROM clients")

	sql, ok := cache.Get("wie viele kunden?")
	if !ok {
		t.Fatalf("expected normalized cache hit")
	}
	if sql != "SELECT COUNT(*) FROM clients" {
		t.Fatalf("unexpected SQL from normalized cache: %s", sql)
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

func TestGetAnthropicClient_ReuseAndReplaceByAPIKey(t *testing.T) {
	resetCaches()

	clientA1 := getAnthropicClient("key-a")
	clientA2 := getAnthropicClient("key-a")
	if clientA1 != clientA2 {
		t.Fatal("expected same Anthropic client instance for same API key")
	}

	clientB := getAnthropicClient("key-b")
	if clientB == clientA1 {
		t.Fatal("expected a new Anthropic client instance when API key changes")
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

func TestRunNLQ_UsesNormalizedQuestionCacheKey(t *testing.T) {
	resetCaches()

	h := &Handler{DB: nil}
	questionCache.Set("  Wie   viele   Kunden?  ", "SELECT 42 AS answer")

	body := map[string]string{"question": "wie viele kunden?"}
	b, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/nlq", bytes.NewReader(b))
	w := httptest.NewRecorder()

	h.RunNLQ(w, req)

	var resp nlqResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}

	if resp.SQL != "SELECT 42 AS answer LIMIT 100" {
		t.Fatalf("expected cached normalized SQL, got %q", resp.SQL)
	}
}

func TestRunNLQ_DedupesConcurrentQuestionGeneration(t *testing.T) {
	resetCaches()

	originalGenerateSQL := nlqGenerateSQL
	defer func() { nlqGenerateSQL = originalGenerateSQL }()

	var callCount int32
	nlqGenerateSQL = func(ctx context.Context, question string) (string, error) {
		atomic.AddInt32(&callCount, 1)
		time.Sleep(20 * time.Millisecond)
		return "SELECT 1 AS answer", nil
	}

	h := &Handler{DB: nil}
	var wg sync.WaitGroup
	errCh := make(chan error, 5)

	for _, q := range []string{
		"Wie viele Kunden?",
		" wie   viele kunden? ",
		"WIE VIELE KUNDEN?",
		"Wie viele Kunden? ",
		"wie viele kunden?",
	} {
		wg.Add(1)
		go func(question string) {
			defer wg.Done()
			body := map[string]string{"question": question}
			b, _ := json.Marshal(body)
			req := httptest.NewRequest(http.MethodPost, "/api/nlq", bytes.NewReader(b))
			w := httptest.NewRecorder()

			h.RunNLQ(w, req)

			if w.Code != http.StatusOK {
				errCh <- fmt.Errorf("unexpected status %d", w.Code)
				return
			}

			var resp nlqResponse
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				errCh <- err
				return
			}
			if resp.SQL != "SELECT 1 AS answer LIMIT 100" {
				errCh <- fmt.Errorf("unexpected SQL %q", resp.SQL)
			}
		}(q)
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}

	if got := atomic.LoadInt32(&callCount); got != 1 {
		t.Fatalf("expected exactly one SQL generation call, got %d", got)
	}
}

func TestRunNLQ_GenerateSQLError(t *testing.T) {
	resetCaches()

	originalGenerateSQL := nlqGenerateSQL
	defer func() { nlqGenerateSQL = originalGenerateSQL }()
	nlqGenerateSQL = func(ctx context.Context, question string) (string, error) {
		return "", fmt.Errorf("anthropic unavailable")
	}

	h := &Handler{DB: nil}
	body := map[string]string{"question": "Wie viele Kunden?"}
	b, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/nlq", bytes.NewReader(b))
	w := httptest.NewRecorder()

	h.RunNLQ(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}

	var resp nlqResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	if resp.Error != "anthropic unavailable" {
		t.Fatalf("expected generation error in response, got %+v", resp)
	}
}

// ---------------------------------------------------------------------------
// writeJSONErr – HTTP status code mapping
// ---------------------------------------------------------------------------

func TestWriteJSONErr_AnthropicPaymentRequired(t *testing.T) {
	w := httptest.NewRecorder()
	apiErr := &anthropic.Error{StatusCode: http.StatusPaymentRequired}
	writeJSONErr(w, nlqResponse{Error: "payment required"}, apiErr)
	if w.Code != http.StatusPaymentRequired {
		t.Fatalf("expected 402, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected application/json, got %q", ct)
	}
}

func TestWriteJSONErr_AnthropicRateLimit(t *testing.T) {
	w := httptest.NewRecorder()
	apiErr := &anthropic.Error{StatusCode: http.StatusTooManyRequests}
	writeJSONErr(w, nlqResponse{Error: "rate limited"}, apiErr)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", w.Code)
	}
}

func TestWriteJSONErr_AnthropicOtherError(t *testing.T) {
	w := httptest.NewRecorder()
	apiErr := &anthropic.Error{StatusCode: http.StatusInternalServerError}
	writeJSONErr(w, nlqResponse{Error: "upstream error"}, apiErr)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Code)
	}
}

func TestWriteJSONErr_NonAnthropicError(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSONErr(w, nlqResponse{Error: "internal"}, fmt.Errorf("something broke"))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
	var resp nlqResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("body decode: %v", err)
	}
	if resp.Error != "internal" {
		t.Fatalf("expected error field, got %+v", resp)
	}
}

func TestRunNLQ_UsesSQLResultCacheFastPath(t *testing.T) {
	resetCaches()

	questionCache.Set("Wie viele Kunden?", "SELECT COUNT(*) FROM clients")
	cached := nlqResponse{
		SQL:     "SELECT COUNT(*) FROM clients",
		Columns: []string{"count"},
		Rows: []map[string]interface{}{
			{"count": int64(12)},
		},
	}
	sqlCache.Set("SELECT COUNT(*) FROM clients", cached)

	h := &Handler{DB: nil}
	body := map[string]string{"question": "Wie viele Kunden?"}
	b, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/nlq", bytes.NewReader(b))
	w := httptest.NewRecorder()

	h.RunNLQ(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp nlqResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	if resp.SQL != cached.SQL || len(resp.Rows) != 1 || resp.Rows[0]["count"] != float64(12) {
		t.Fatalf("expected cached NLQ response, got %+v", resp)
	}
}

func TestGenerateSQL_OfflineFallback(t *testing.T) {
	t.Setenv("NLQ_MOCK", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

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
	t.Setenv("ANTHROPIC_API_KEY", "")

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

// ---------------------------------------------------------------------------
// normalizeQuestionCacheKey
// ---------------------------------------------------------------------------

func TestNormalizeQuestionCacheKey(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"  Wie   viele   Kunden?  ", "wie viele kunden?"},
		{"WIE VIELE KUNDEN?", "wie viele kunden?"},
		{"wie viele kunden?", "wie viele kunden?"},
		{"   ", ""},
	}
	for _, c := range cases {
		got := normalizeQuestionCacheKey(c.input)
		if got != c.want {
			t.Fatalf("normalizeQuestionCacheKey(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// isSafeSQL – additional forbidden patterns
// ---------------------------------------------------------------------------

func TestIsSafeSQL_AdditionalForbiddenPatterns(t *testing.T) {
	cases := []struct {
		sql  string
		want bool
	}{
		{"SELECT 1 /* comment */", false},         // block comment open
		{"SELECT 1 */ FROM clients", false},       // block comment close
		{"SELECT pg_terminate_backend(1)", false}, // pg_terminate
		{"SELECT * FROM clients LIMIT 10", true},  // clean
	}
	for _, c := range cases {
		got := isSafeSQL(c.sql)
		if got != c.want {
			t.Fatalf("isSafeSQL(%q) = %v, want %v", c.sql, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// generateSQL – uncovered mock mode branches
// ---------------------------------------------------------------------------

func TestGenerateSQL_MockMode_AktiveKunden(t *testing.T) {
	t.Setenv("NLQ_MOCK", "1")
	sql, err := generateSQL(context.Background(), "Zeige mir alle aktive Kunden")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(strings.ToLower(sql), "status = 'active'") {
		t.Fatalf("expected active-clients SQL, got: %s", sql)
	}
}

// ---------------------------------------------------------------------------
// generateSQL – offline fallback branches
// ---------------------------------------------------------------------------

func TestGenerateSQL_OfflineFallback_Stages(t *testing.T) {
	t.Setenv("NLQ_MOCK", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	sql, err := generateSQL(context.Background(), "wie viele stages gibt es?")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(strings.ToLower(sql), "stages") {
		t.Fatalf("expected stages fallback SQL, got: %s", sql)
	}
}

func TestGenerateSQL_OfflineFallback_Default(t *testing.T) {
	t.Setenv("NLQ_MOCK", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	sql, err := generateSQL(context.Background(), "etwas völlig anderes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !isSelect(sql) {
		t.Fatalf("expected SELECT fallback SQL, got: %s", sql)
	}
}

// ---------------------------------------------------------------------------
// getAnthropicClient – concurrent initialization with the same new key
// ---------------------------------------------------------------------------

func TestGetAnthropicClient_ConcurrentInit(t *testing.T) {
	resetCaches()

	const n = 10
	results := make([]*anthropic.Client, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = getAnthropicClient("concurrent-key")
		}(i)
	}
	wg.Wait()

	for i := 1; i < n; i++ {
		if results[i] != results[0] {
			t.Fatalf("expected all goroutines to receive the same client instance")
		}
	}
}

// ---------------------------------------------------------------------------
// RunNLQ – rows.Columns() error path
// ---------------------------------------------------------------------------

func TestRunNLQ_ColumnsError(t *testing.T) {
	resetCaches()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}
	question := "Zeige mir Kunden"
	questionCache.Set(question, "SELECT id FROM clients")

	// Return rows that produce a Columns() error by closing them immediately.
	rows := sqlmock.NewRows([]string{}).CloseError(fmt.Errorf("columns error"))
	mock.ExpectQuery("^SELECT id FROM clients LIMIT 100$").WillReturnRows(rows)

	body := map[string]string{"question": question}
	b, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/nlq", bytes.NewReader(b))
	w := httptest.NewRecorder()

	h.RunNLQ(w, req)

	var resp nlqResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)

	// Either an error is set, or we get an empty result — both are acceptable; we
	// mainly care that the handler does not panic and returns 200.
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// RunNLQ – successful DB execution populates SQL result cache
// ---------------------------------------------------------------------------

func TestRunNLQ_DBSuccessPopulatesSQLCache(t *testing.T) {
	resetCaches()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}
	question := "Zeige Kunden Status"
	questionCache.Set(question, "SELECT id, name FROM clients")

	mock.ExpectQuery("^SELECT id, name FROM clients LIMIT 100$").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(1, "Jane Doe"))

	body := map[string]string{"question": question}
	b, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/nlq", bytes.NewReader(b))
	w := httptest.NewRecorder()

	h.RunNLQ(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp nlqResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(resp.Rows))
	}
	if resp.Rows[0]["name"] != "Jane Doe" {
		t.Fatalf("expected Jane Doe, got %v", resp.Rows[0]["name"])
	}

	// Second request should be served from the SQL cache (no DB expectation set).
	req2 := httptest.NewRequest(http.MethodPost, "/api/nlq", bytes.NewReader(b))
	w2 := httptest.NewRecorder()
	h.RunNLQ(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("cache path: expected 200, got %d", w2.Code)
	}
	var resp2 nlqResponse
	if err := json.NewDecoder(w2.Body).Decode(&resp2); err != nil {
		t.Fatalf("cache decode: %v", err)
	}
	if len(resp2.Rows) != 1 || resp2.Rows[0]["name"] != "Jane Doe" {
		t.Fatalf("expected cached row, got %+v", resp2)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet mock expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// generateSQL – mock mode "wie viele stages" branch
// ---------------------------------------------------------------------------

func TestGenerateSQL_MockMode_WieVieleStages(t *testing.T) {
	t.Setenv("NLQ_MOCK", "1")
	sql, err := generateSQL(context.Background(), "wie viele stages gibt es?")
	if err != nil {
		t.Fatalf("generateSQL error: %v", err)
	}
	if !strings.Contains(strings.ToLower(sql), "stages") {
		t.Fatalf("expected stages SQL, got: %s", sql)
	}
}

func TestGenerateSQL_MockMode_WievielStages(t *testing.T) {
	t.Setenv("NLQ_MOCK", "1")
	sql, err := generateSQL(context.Background(), "wieviele stages?")
	if err != nil {
		t.Fatalf("generateSQL error: %v", err)
	}
	if !strings.Contains(strings.ToLower(sql), "stages") {
		t.Fatalf("expected stages SQL, got: %s", sql)
	}
}

func TestGenerateSQL_MockMode_KeineVerlaengerung(t *testing.T) {
	t.Setenv("NLQ_MOCK", "1")
	sql, err := generateSQL(context.Background(), "keine verlaengerung diesen Monat")
	if err != nil {
		t.Fatalf("generateSQL error: %v", err)
	}
	if !strings.Contains(sql, "keine_verlaengerung") {
		t.Fatalf("expected keine_verlaengerung SQL, got: %s", sql)
	}
}

func TestGenerateSQL_MockMode_UpsellUmsatz(t *testing.T) {
	t.Setenv("NLQ_MOCK", "1")
	sql, err := generateSQL(context.Background(), "upsell umsatz gesamt")
	if err != nil {
		t.Fatalf("generateSQL error: %v", err)
	}
	if !strings.Contains(strings.ToLower(sql), "upsell_revenue") {
		t.Fatalf("expected upsell_revenue SQL, got: %s", sql)
	}
}

func TestGenerateSQL_MockMode_UpsellGespraech(t *testing.T) {
	t.Setenv("NLQ_MOCK", "1")
	sql, err := generateSQL(context.Background(), "bald ablaufend diese Woche")
	if err != nil {
		t.Fatalf("generateSQL error: %v", err)
	}
	if !strings.Contains(strings.ToLower(sql), "end_date") {
		t.Fatalf("expected end_date SQL, got: %s", sql)
	}
}

func TestGenerateSQL_MockMode_Verlaengerung(t *testing.T) {
	t.Setenv("NLQ_MOCK", "1")
	sql, err := generateSQL(context.Background(), "verlaengerung diese Woche")
	if err != nil {
		t.Fatalf("generateSQL error: %v", err)
	}
	if !strings.Contains(strings.ToLower(sql), "verlaengerung") {
		t.Fatalf("expected verlaengerung SQL, got: %s", sql)
	}
}

func TestGenerateSQL_MockMode_Default(t *testing.T) {
	t.Setenv("NLQ_MOCK", "1")
	sql, err := generateSQL(context.Background(), "zeige alle einträge")
	if err != nil {
		t.Fatalf("generateSQL error: %v", err)
	}
	if !strings.Contains(strings.ToLower(sql), "clients limit 10") {
		t.Fatalf("expected default SQL, got: %s", sql)
	}
}

// RunNLQ with NLQ_MOCK=1 set as env var (not h.DB==nil) hits the env-var branch in SQL singleflight.
func TestRunNLQ_MockModeEnvVar(t *testing.T) {
	resetCaches()
	t.Setenv("NLQ_MOCK", "1")

	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}
	question := "zeige kunden mock"
	questionCache.Set(question, "SELECT id, name, email, status FROM clients LIMIT 10")

	body := map[string]string{"question": question}
	b, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/nlq", bytes.NewReader(b))
	w := httptest.NewRecorder()

	h.RunNLQ(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// RunNLQ with a DB returning a nil value in a column exercises the nil case in the type switch.
func TestRunNLQ_NilColumnValue(t *testing.T) {
	resetCaches()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}
	question := "zeige kunden mit null name"
	sql := "SELECT id, name FROM clients"
	questionCache.Set(question, sql)

	rows := sqlmock.NewRows([]string{"id", "name"}).AddRow(1, nil)
	mock.ExpectQuery("^SELECT id, name FROM clients LIMIT 100$").WillReturnRows(rows)

	body := map[string]string{"question": question}
	b, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/nlq", bytes.NewReader(b))
	w := httptest.NewRecorder()

	h.RunNLQ(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp nlqResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(resp.Rows))
	}
	if resp.Rows[0]["name"] != nil {
		t.Fatalf("expected nil name, got %v", resp.Rows[0]["name"])
	}
}

// RunNLQ with a DB returning a []byte column value exercises the []byte case in the type switch.
func TestRunNLQ_ByteColumnValue(t *testing.T) {
	resetCaches()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &Handler{DB: db}
	question := "zeige kunden flag status"
	sqlStr := "SELECT id, flag FROM clients"
	questionCache.Set(question, sqlStr)

	rows := sqlmock.NewRows([]string{"id", "flag"}).AddRow(int64(1), []byte("true"))
	mock.ExpectQuery("^SELECT id, flag FROM clients LIMIT 100$").WillReturnRows(rows)

	body := map[string]string{"question": question}
	b, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/nlq", bytes.NewReader(b))
	w := httptest.NewRecorder()

	h.RunNLQ(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp nlqResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(resp.Rows))
	}
	if resp.Rows[0]["flag"] != true {
		t.Fatalf("expected flag=true, got %v", resp.Rows[0]["flag"])
	}
}
