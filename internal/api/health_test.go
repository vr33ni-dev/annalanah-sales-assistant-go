package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/api"
)

func TestHealthCheck(t *testing.T) {
	// Arrange: create a fake handler with a known config
	h := &api.Handler{
		Cfg: &api.Config{
			AppEnv: "test",
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()

	// Act: call the method under test
	h.Health(w, req)

	// Assert: validate response
	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %s", ct)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if body["ok"] != true {
		t.Errorf("expected ok=true, got %v", body["ok"])
	}
	if body["status"] != "healthy" {
		t.Errorf("expected status=healthy, got %v", body["status"])
	}
	if body["env"] != "test" {
		t.Errorf("expected env=test, got %v", body["env"])
	}
}
