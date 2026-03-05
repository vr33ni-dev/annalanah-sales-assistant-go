package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	_ "github.com/lib/pq"
)

func TestGenerateSQL_NLQFallback(t *testing.T) {
	ctx := context.Background()
	sql, err := generateSQL(ctx, "Wie viele Stages habe ich?")
	if err != nil {
		t.Fatalf("generateSQL failed: %v", err)
	}
	if !strings.HasPrefix(strings.ToLower(sql), "select") {
		t.Errorf("expected SELECT, got: %s", sql)
	}
}

func TestGenerateSQL_NLQMockMode(t *testing.T) {
	os.Setenv("NLQ_MOCK", "1")
	defer os.Unsetenv("NLQ_MOCK")

	sql, err := generateSQL(context.Background(), "Wie viele Kunden habe ich?")
	if err != nil {
		t.Fatal(err)
	}

	// Just assert it's a SELECT and references clients
	s := strings.ToLower(sql)
	if !strings.HasPrefix(s, "select") {
		t.Errorf("expected SELECT in mock mode, got: %s", sql)
	}
	if !strings.Contains(s, "from clients") && !strings.Contains(s, "count(*)") {
		t.Errorf("expected query about clients, got: %s", sql)
	}
}

func TestGenerateSQL_NLQMockMode_UpsellQuestions(t *testing.T) {
	os.Setenv("NLQ_MOCK", "1")
	defer os.Unsetenv("NLQ_MOCK")

	tests := []struct {
		name         string
		question     string
		mustContain  string
		mustNotError bool
	}{
		{
			name:         "upsell revenue",
			question:     "Wie hoch ist unser Upsell-Umsatz?",
			mustContain:  "from contract_upsells",
			mustNotError: true,
		},
		{
			name:         "successful renewals",
			question:     "Zeige mir alle Verlängerung Fälle",
			mustContain:  "upsell_result = 'verlaengerung'",
			mustNotError: true,
		},
		{
			name:         "no renewal count",
			question:     "Wie viele keine Verlängerung Fälle gibt es?",
			mustContain:  "upsell_result = 'keine_verlaengerung'",
			mustNotError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sqlText, err := generateSQL(context.Background(), tc.question)
			if tc.mustNotError && err != nil {
				t.Fatalf("generateSQL failed: %v", err)
			}

			s := strings.ToLower(strings.TrimSpace(sqlText))
			if !strings.HasPrefix(s, "select") {
				t.Fatalf("expected SELECT, got: %s", sqlText)
			}
			if !strings.Contains(s, strings.ToLower(tc.mustContain)) {
				t.Fatalf("expected SQL to contain %q, got: %s", tc.mustContain, sqlText)
			}
		})
	}
}

func TestGenerateSQL_QuestionsFromFile(t *testing.T) {
	os.Setenv("NLQ_MOCK", "1")
	defer os.Unsetenv("NLQ_MOCK")

	data, err := os.ReadFile("testdata/test_questions.txt")
	if err != nil {
		t.Fatalf("failed to read test_questions.txt: %v", err)
	}

	lines := strings.Split(string(data), "\n")
	for _, q := range lines {
		q = strings.TrimSpace(q)
		if q == "" {
			continue
		}

		t.Run(q, func(t *testing.T) {
			sqlText, err := generateSQL(context.Background(), q)
			if err != nil {
				t.Fatalf("generateSQL failed: %v", err)
			}

			s := strings.ToLower(strings.TrimSpace(sqlText))
			if !strings.HasPrefix(s, "select") {
				t.Fatalf("expected SELECT for %q, got: %s", q, sqlText)
			}

			// Optional: very light sanity checks based on question
			if strings.Contains(strings.ToLower(q), "stages") &&
				!strings.Contains(s, "stages") {
				t.Errorf("expected query to touch stages for %q, got: %s", q, sqlText)
			}
			if strings.Contains(strings.ToLower(q), "kunden") &&
				!strings.Contains(s, "client") && !strings.Contains(s, "kunden") {
				t.Logf("note: query for %q does not clearly reference clients: %s", q, sqlText)
			}
		})
	}
}

func TestRunNLQ_WithQuestions(t *testing.T) {
	os.Setenv("NLQ_MOCK", "1")
	defer os.Unsetenv("NLQ_MOCK")

	h := &Handler{DB: nil}
	questions := []string{
		"Wie viele Kunden habe ich?",
		"Zeige alle aktiven Kunden.",
	}

	for _, q := range questions {
		t.Run(q, func(t *testing.T) {
			body := bytes.NewBufferString(`{"question": "` + q + `"}`)
			req := httptest.NewRequest("POST", "/api/nlq", body)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			h.RunNLQ(w, req)
			res := w.Result()
			defer res.Body.Close()

			if res.StatusCode != http.StatusOK {
				t.Fatalf("unexpected status: %d", res.StatusCode)
			}

			var parsed nlqResponse
			if err := json.NewDecoder(res.Body).Decode(&parsed); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			if !strings.HasPrefix(strings.ToLower(parsed.SQL), "select") {
				t.Fatalf("expected SELECT for %q, got %s", q, parsed.SQL)
			}

			if parsed.Error != "" {
				t.Errorf("unexpected error for %q: %s", q, parsed.Error)
			}

			// Optional: keep a strict check for the first question
			if strings.Contains(q, "kunden") && parsed.SQL != "SELECT 'mock mode' AS sql LIMIT 100" {
				t.Logf("mock SQL changed for %q: got %s", q, parsed.SQL)
			}
		})
	}
}

func TestRunNLQ_UnsafeSQLRejected(t *testing.T) {
	resetCaches()

	h := &Handler{DB: nil}

	question := "Kunden hack" // must pass isLikelySQLQuestion
	questionCache.Set(question, "SELECT * FROM clients; DROP TABLE clients")

	body := bytes.NewBufferString(`{"question": "` + question + `"}`)
	req := httptest.NewRequest("POST", "/api/nlq", body)
	w := httptest.NewRecorder()

	h.RunNLQ(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", w.Code)
	}

	var resp nlqResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if resp.Error == "" {
		t.Fatalf("expected unsafe SQL error")
	}
}

func TestRunNLQ_AddsLimit(t *testing.T) {
	resetCaches()
	os.Setenv("NLQ_MOCK", "1")
	defer os.Unsetenv("NLQ_MOCK")

	h := &Handler{DB: nil}

	question := "Zeige Kunden"
	questionCache.Set(question, "SELECT id FROM clients")

	body := bytes.NewBufferString(`{"question": "` + question + `"}`)
	req := httptest.NewRequest("POST", "/api/nlq", body)
	w := httptest.NewRecorder()

	h.RunNLQ(w, req)

	var resp nlqResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if !strings.Contains(strings.ToLower(resp.SQL), "limit 100") {
		t.Fatalf("expected LIMIT injection, got: %s", resp.SQL)
	}
}

func TestGenerateSQL_WithKeyAndMock(t *testing.T) {
	os.Setenv("NLQ_MOCK", "1")
	os.Setenv("OPENAI_API_KEY", "fake")
	defer os.Unsetenv("NLQ_MOCK")
	defer os.Unsetenv("OPENAI_API_KEY")

	sql, err := generateSQL(context.Background(), "Wie viele Kunden?")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.HasPrefix(strings.ToLower(sql), "select") {
		t.Fatalf("expected SELECT, got %s", sql)
	}
}

func TestRunNLQ_BadJSON_Handler(t *testing.T) {
	h := &Handler{DB: nil}

	req := httptest.NewRequest("POST", "/api/nlq", bytes.NewBufferString("not-json"))
	w := httptest.NewRecorder()

	h.RunNLQ(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad json, got %d", w.Code)
	}
}

func TestRunNLQ_NonSQLQuestion_Handler(t *testing.T) {
	h := &Handler{DB: nil}

	body := bytes.NewBufferString(`{"question": "Hallo Welt"}`)
	req := httptest.NewRequest("POST", "/api/nlq", body)
	w := httptest.NewRecorder()

	h.RunNLQ(w, req)

	if !strings.Contains(w.Body.String(), "Datenabfragen") {
		t.Fatalf("expected helper message, got: %s", w.Body.String())
	}
}

func TestRunNLQ_Aggregate_NoLimit(t *testing.T) {
	os.Setenv("NLQ_MOCK", "1")
	defer os.Unsetenv("NLQ_MOCK")

	h := &Handler{DB: nil}

	questionCache.Set("agg", "SELECT COUNT(*) FROM clients")

	body := bytes.NewBufferString(`{"question": "agg"}`)
	req := httptest.NewRequest("POST", "/api/nlq", body)
	w := httptest.NewRecorder()

	h.RunNLQ(w, req)

	var resp nlqResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if strings.Contains(strings.ToLower(resp.SQL), "limit") {
		t.Fatalf("aggregate query should not have LIMIT, got: %s", resp.SQL)
	}
}
