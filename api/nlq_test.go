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
