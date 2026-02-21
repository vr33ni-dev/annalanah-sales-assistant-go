package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNullHelpers(t *testing.T) {
	// nullStr
	ns := nullStr("")
	if ns.Valid {
		t.Fatalf("expected empty nullStr to be invalid")
	}
	ns2 := nullStr("x")
	if !ns2.Valid || ns2.String != "x" {
		t.Fatalf("expected valid nullStr with value 'x'")
	}

	// nullInt
	var p *int
	ni := nullInt(p)
	if ni.Valid {
		t.Fatalf("expected nil nullInt to be invalid")
	}
	v := 7
	ni2 := nullInt(&v)
	if !ni2.Valid || ni2.Int64 != int64(v) {
		t.Fatalf("expected valid nullInt with value 7")
	}

	// nullTime
	var tt *time.Time
	nt := nullTime(tt)
	if nt.Valid {
		t.Fatalf("expected nil nullTime to be invalid")
	}
	now := time.Date(2025, 10, 5, 12, 0, 0, 0, time.UTC)
	nt2 := nullTime(&now)
	if !nt2.Valid || !nt2.Time.Equal(now) {
		t.Fatalf("expected valid nullTime with matching time")
	}
}

func TestParseIDFromURL(t *testing.T) {
	id, ok := parseIDFromURL("/api/clients/123")
	if !ok || id != 123 {
		t.Fatalf("expected id 123 parsed")
	}

	_, ok = parseIDFromURL("/nope")
	if ok {
		t.Fatalf("expected parse failure for short path")
	}

	_, ok = parseIDFromURL("/api/items/notanint")
	if ok {
		t.Fatalf("expected parse failure for non-int id")
	}
}

func TestWriteJSONError(t *testing.T) {
	w := httptest.NewRecorder()
	WriteJSONErrorForTest(w, "bad", http.StatusBadRequest)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}
	body := strings.TrimSpace(w.Body.String())
	if !strings.Contains(body, `"error"`) || !strings.Contains(body, "bad") {
		t.Fatalf("unexpected body: %s", body)
	}
}
