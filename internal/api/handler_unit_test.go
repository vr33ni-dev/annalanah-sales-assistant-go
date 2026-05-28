package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ── WriteJSONError ────────────────────────────────────────────────────────────────

func TestWriteJSONError(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSONError(w, "something went wrong", http.StatusBadRequest)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "something went wrong") {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

func TestParseIDFromURLForTest(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		wantID int
		wantOK bool
	}{
		{"valid ID", "/clients/123", 123, true},
		{"invalid ID", "/clients/abc", 0, false},
		{"missing ID", "/clients/", 0, false},
		{"too short", "/clients", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, ok := parseIDFromURL(tt.path)
			if id != tt.wantID || ok != tt.wantOK {
				t.Errorf("got (%d, %v), want (%d, %v)", id, ok, tt.wantID, tt.wantOK)
			}
		})
	}
}
