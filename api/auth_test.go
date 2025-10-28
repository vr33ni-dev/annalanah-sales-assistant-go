package api_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/vr33ni-dev/annalanah-sales-assistant-go/api"
)

// --- helpers ---

func newTestAuth() *api.Auth {
	key := []byte("12345678901234567890123456789012") // 32 bytes
	return &api.Auth{
		CookieName: "app_session",
		CookieKey:  key,
		Allowed: map[string]bool{
			"user@example.com": true,
		},
	}
}

// --- tests ---

func TestMakeCookieAndParseSession(t *testing.T) {
	auth := newTestAuth()
	h := &api.Handler{Auth: auth}

	sess := api.Session{
		Email: "user@example.com",
		Name:  "Alice",
		Exp:   time.Now().Add(1 * time.Hour),
	}
	cookie := auth.MakeCookieForTest(sess, true)
	if cookie == nil || cookie.Value == "" {
		t.Fatal("expected cookie to be created")
	}

	// Create request containing that cookie
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.AddCookie(cookie)

	parsed, ok := h.ParseSessionForTest(req) // helper wrapper we'll define below
	if !ok {
		t.Fatal("expected session to parse correctly")
	}
	if parsed.Email != sess.Email || parsed.Name != sess.Name {
		t.Fatalf("unexpected session parsed: %+v", parsed)
	}
}

func TestParseSessionTampered(t *testing.T) {
	auth := newTestAuth()
	h := &api.Handler{Auth: auth}

	sess := api.Session{Email: "user@example.com", Name: "Alice", Exp: time.Now().Add(1 * time.Hour)}
	cookie := auth.MakeCookieForTest(sess, true)

	// Split cookie into payload and signature
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}

	// Flip one character in the signature (invalidating it)
	sigBytes := []byte(parts[1])
	if sigBytes[0] == 'A' {
		sigBytes[0] = 'B'
	} else {
		sigBytes[0] = 'A'
	}
	parts[1] = string(sigBytes)
	tamperedValue := strings.Join(parts, ".")

	tampered := *cookie
	tampered.Value = tamperedValue

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&tampered)

	if _, ok := h.ParseSessionForTest(req); ok {
		t.Fatal("expected tampered cookie to fail validation")
	}
}

func TestIsSecure(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if api.IsSecureForTest(req) {
		t.Fatal("expected insecure without TLS or https headers")
	}
	req.Header.Set("X-Forwarded-Proto", "https")
	if !api.IsSecureForTest(req) {
		t.Fatal("expected secure when X-Forwarded-Proto=https")
	}
}

func TestHandleLogoutClearsCookies(t *testing.T) {
	auth := newTestAuth()
	h := &api.Handler{Auth: auth}

	req := httptest.NewRequest(http.MethodGet, "/auth/logout", nil)
	w := httptest.NewRecorder()

	os.Setenv("POST_LOGOUT_REDIRECT", "https://frontend.test")

	h.HandleLogoutForTest(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected redirect, got %d", resp.StatusCode)
	}

	cookies := resp.Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected cookies to be cleared")
	}
	found := false
	for _, c := range cookies {
		if c.Name == auth.CookieName && c.MaxAge == -1 {
			found = true
		}
	}
	if !found {
		t.Fatal("expected session cookie to be cleared")
	}
}
