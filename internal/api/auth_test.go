package api_test

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/api"
	authpkg "github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/auth"
	"golang.org/x/oauth2"
)

// --- helpers ---

func newTestAuth() *authpkg.Auth {
	key := []byte("12345678901234567890123456789012") // 32 bytes
	return &authpkg.Auth{
		CookieName: "app_session",
		CookieKey:  key,
		Allowed: map[string]bool{
			"user@example.com": true,
		},
		OAuth: &oauth2.Config{
			ClientID:     "test-client-id",
			ClientSecret: "test-secret",
			RedirectURL:  "https://example.com/auth/callback",
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://example.com/auth",
				TokenURL: "https://example.com/token",
			},
		},
	}
}

// --- tests ---

func TestMakeCookieAndParseSession(t *testing.T) {
	auth := newTestAuth()
	h := &api.Handler{Auth: auth}

	sess := authpkg.Session{
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

	sess := authpkg.Session{Email: "user@example.com", Name: "Alice", Exp: time.Now().Add(1 * time.Hour)}
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
	if authpkg.IsSecureForTest(req) {
		t.Fatal("expected insecure without TLS or https headers")
	}
	req.Header.Set("X-Forwarded-Proto", "https")
	if !authpkg.IsSecureForTest(req) {
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

func TestNewAuth(t *testing.T) {
	os.Setenv("ALLOWED_EMAILS", "a@example.com,b@example.com")
	os.Setenv("COOKIE_SIGNING_KEY", "12345678901234567890123456789012")
	os.Setenv("GOOGLE_CLIENT_ID", "cid")
	os.Setenv("GOOGLE_CLIENT_SECRET", "secret")
	os.Setenv("OAUTH_REDIRECT_URL", "https://frontend/auth/google/callback")

	a, err := authpkg.NewAuth()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a == nil || a.OAuth == nil {
		t.Fatal("expected Auth to be initialized")
	}

	// failure branch: short key
	os.Setenv("COOKIE_SIGNING_KEY", "short")
	if _, err := authpkg.NewAuth(); err == nil {
		t.Fatal("expected error for short key")
	}
}

func TestRandState(t *testing.T) {
	s := authpkg.RandStateForTest() // or expose randState through a test wrapper
	if s == "" {
		t.Fatal("expected non-empty random state")
	}
	if _, err := base64.RawURLEncoding.DecodeString(s); err != nil {
		t.Fatalf("expected valid base64, got %v", err)
	}
}

func TestMountAuthRoutes(t *testing.T) {
	h := &api.Handler{Auth: newTestAuth()}
	r := chi.NewRouter()
	h.MountAuthRoutes(r)

	// Test that routes mount and don't panic for a harmless endpoint
	req := httptest.NewRequest("GET", "/api/me", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code == 0 {
		t.Fatal("expected router to respond")
	}
}

func TestMeHandlerAndRequireAuth(t *testing.T) {
	h := &api.Handler{Auth: newTestAuth()}
	sess := authpkg.Session{Email: "user@example.com", Name: "Alice", Exp: time.Now().Add(time.Hour)}
	c := h.Auth.MakeCookieForTest(sess, false)

	req := httptest.NewRequest("GET", "/api/me", nil)
	req.AddCookie(c)
	w := httptest.NewRecorder()
	h.MeHandlerForTest(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Result().StatusCode)
	}

	// test RequireAuth with missing cookie (unauthorized)
	protected := h.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req2 := httptest.NewRequest("GET", "/protected", nil)
	w2 := httptest.NewRecorder()
	protected.ServeHTTP(w2, req2)
	if w2.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w2.Result().StatusCode)
	}
}

func TestHandleAuthCallback_StateMismatch(t *testing.T) {
	h := &api.Handler{Auth: newTestAuth()}
	req := httptest.NewRequest("GET", "/auth/google/callback?state=bad", nil)
	req.AddCookie(&http.Cookie{Name: "oauth_state", Value: "good"})
	w := httptest.NewRecorder()
	h.HandleAuthCallbackForTest(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad state, got %d", w.Result().StatusCode)
	}
}

func TestHandleAuthStart(t *testing.T) {
	h := &api.Handler{Auth: newTestAuth()}
	req := httptest.NewRequest("GET", "/auth/google?redirect=/dashboard", nil)
	w := httptest.NewRecorder()

	h.HandleAuthStartForTest(w, req) // simple wrapper for tests
	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected redirect, got %d", resp.StatusCode)
	}
	foundState := false
	for _, c := range resp.Cookies() {
		if c.Name == "oauth_state" {
			foundState = true
		}
	}
	if !foundState {
		t.Fatal("expected oauth_state cookie")
	}
}
