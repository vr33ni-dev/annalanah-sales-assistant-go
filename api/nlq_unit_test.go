package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

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

func TestRunNLQ_Handler_MockMode(t *testing.T) {
	t.Setenv("NLQ_MOCK", "1")
	h := &Handler{DB: nil}

	body := map[string]string{"question": "Wie viele Kunden?"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/nlq", bytes.NewReader(b))
	req = req.WithContext(context.Background())
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

func TestRunNLQ_Handler_BadJSON(t *testing.T) {
	h := &Handler{DB: nil}
	req := httptest.NewRequest(http.MethodPost, "/api/nlq", bytes.NewReader([]byte("not json")))
	w := httptest.NewRecorder()
	h.RunNLQ(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad json, got %d", w.Code)
	}
}
