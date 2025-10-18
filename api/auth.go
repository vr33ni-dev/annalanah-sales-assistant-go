// file: api/auth.go
package api

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/idtoken"
)

type Session struct {
	Email string    `json:"email"`
	Name  string    `json:"name"`
	Exp   time.Time `json:"exp"`
}

type Auth struct {
	OAuth      *oauth2.Config
	Allowed    map[string]bool
	CookieName string
	CookieKey  []byte
}

func (h *Handler) InitAuth() error {
	allowed := map[string]bool{}
	for _, e := range strings.Split(os.Getenv("ALLOWED_EMAILS"), ",") {
		e = strings.ToLower(strings.TrimSpace(e))
		if e != "" {
			allowed[e] = true
		}
	}
	key := []byte(os.Getenv("COOKIE_SIGNING_KEY"))
	if len(key) < 32 {
		return errors.New("COOKIE_SIGNING_KEY must be >=32 bytes")
	}
	h.Auth = &Auth{
		OAuth: &oauth2.Config{
			ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
			ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
			RedirectURL:  os.Getenv("OAUTH_REDIRECT_URL"), // <- must be FRONTEND origin + /auth/google/callback
			Scopes:       []string{"openid", "email", "profile"},
			Endpoint:     google.Endpoint,
		},
		Allowed:    allowed,
		CookieName: "app_session",
		CookieKey:  key,
	}
	return nil
}

func (a *Auth) sign(b []byte) string {
	h := hmac.New(sha256.New, a.CookieKey)
	h.Write(b)
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

func isSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		return true
	}
	// fallbacks for envs
	if strings.HasPrefix(os.Getenv("OAUTH_REDIRECT_URL"), "https://") {
		return true
	}
	if strings.HasPrefix(os.Getenv("POST_LOGIN_REDIRECT"), "https://") {
		return true
	}
	return false
}

func (a *Auth) makeCookie(sess Session, secure bool) *http.Cookie {
	payload, _ := json.Marshal(sess)
	enc := base64.RawURLEncoding.EncodeToString(payload)
	token := enc + "." + a.sign([]byte(enc))
	return &http.Cookie{
		Name:     a.CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,               // ⬅️ no longer hardcoded true
		SameSite: http.SameSiteLaxMode, // first-party
		Expires:  sess.Exp,
	}
}

func randState() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:])
}

// --- Routes mounting ---

func (h *Handler) MountAuthRoutes(r chi.Router) {
	r.Get("/auth/google", h.handleAuthStart)
	r.Get("/auth/google/callback", h.handleAuthCallback)
	r.Post("/api/logout", h.handleLogout)
	r.Get("/api/me", h.meHandler)
}

// --- Handlers ---

func (h *Handler) handleAuthStart(w http.ResponseWriter, r *http.Request) {
	log.Printf("handleAuthStart: query redirect=%q remote=%s\n", r.URL.Query().Get("redirect"), r.RemoteAddr)

	state := randState()
	secure := isSecure(r)
	sameSite := http.SameSiteLaxMode

	// optional debug flag
	if r.URL.Query().Get("debug") == "1" {
		http.SetCookie(w, &http.Cookie{
			Name:     "oauth_debug",
			Value:    "1",
			Path:     "/",
			HttpOnly: true,
			Secure:   secure,
			SameSite: sameSite,
			Expires:  time.Now().Add(5 * time.Minute),
		})
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    state,
		Path:     "/",
		HttpOnly: false,
		Secure:   secure,
		SameSite: sameSite,
		Expires:  time.Now().Add(10 * time.Minute),
	})

	if redirect := r.URL.Query().Get("redirect"); redirect != "" {
		http.SetCookie(w, &http.Cookie{
			Name:     "post_login_redirect",
			Value:    redirect,
			Path:     "/",
			HttpOnly: true,
			Secure:   secure,
			SameSite: sameSite,
			Expires:  time.Now().Add(10 * time.Minute),
		})
	}

	http.Redirect(w, r, h.Auth.OAuth.AuthCodeURL(state, oauth2.AccessTypeOnline), http.StatusFound)
}

func (h *Handler) handleAuthCallback(w http.ResponseWriter, r *http.Request) {
	log.Printf("handleAuthCallback: host=%q xf-host=%q proto=%q rawQuery=%q remote=%s",
		r.Host, r.Header.Get("X-Forwarded-Host"), r.Header.Get("X-Forwarded-Proto"), r.URL.RawQuery, r.RemoteAddr)

	secure := isSecure(r)
	sameSite := http.SameSiteLaxMode

	// --- CSRF state check
	state := r.URL.Query().Get("state")
	stateC, _ := r.Cookie("oauth_state")
	if state == "" || stateC == nil || state != stateC.Value {
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}

	// --- Exchange code
	code := r.URL.Query().Get("code")
	tok, err := h.Auth.OAuth.Exchange(r.Context(), code)
	if err != nil {
		http.Error(w, "exchange failed", http.StatusUnauthorized)
		return
	}

	// --- Verify ID token
	rawID, _ := tok.Extra("id_token").(string)
	payload, err := idtoken.Validate(r.Context(), rawID, h.Auth.OAuth.ClientID)
	if err != nil {
		http.Error(w, "id token invalid", http.StatusUnauthorized)
		return
	}

	email, _ := payload.Claims["email"].(string)
	name, _ := payload.Claims["name"].(string)
	verified, _ := payload.Claims["email_verified"].(bool)
	email = strings.ToLower(strings.TrimSpace(email))
	if !verified || !h.Auth.Allowed[email] {
		http.Error(w, "access denied", http.StatusForbidden)
		return
	}

	// --- Issue session cookie (host-only on FRONTEND origin)
	ck := h.Auth.makeCookie(Session{
		Email: email,
		Name:  name,
		Exp:   time.Now().Add(12 * time.Hour),
	}, secure)
	http.SetCookie(w, ck)

	// --- Decide final redirect target
	redirectTo := os.Getenv("POST_LOGIN_REDIRECT")
	if redirectTo == "" {
		redirectTo = "/"
	}
	if rc, err := r.Cookie("post_login_redirect"); err == nil && rc.Value != "" {
		redirectTo = rc.Value
		// clear helper cookie
		http.SetCookie(w, &http.Cookie{
			Name:     "post_login_redirect",
			Value:    "",
			Path:     "/",
			HttpOnly: true,
			Secure:   secure,
			SameSite: sameSite,
			Expires:  time.Unix(0, 0),
			MaxAge:   -1,
		})
	}

	log.Printf("auth callback 302 → %q (Host=%q)", redirectTo, r.Host)

	// --- 302 redirect straight to SPA (avoid HTML parsing)
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, redirectTo, http.StatusFound)
}

// in auth.go
func (h *Handler) handleLogout(w http.ResponseWriter, r *http.Request) {
	expired := time.Unix(0, 0)
	secure := isSecure(r)

	wipe := func(sameSite http.SameSite) {
		http.SetCookie(w, &http.Cookie{
			Name:     h.Auth.CookieName,
			Value:    "",
			Path:     "/",
			HttpOnly: true,
			Secure:   secure || sameSite == http.SameSiteNoneMode, // required when None
			SameSite: sameSite,
			Expires:  expired,
			MaxAge:   -1,
		})
	}

	// Clear the common permutations (host-only)
	wipe(http.SameSiteLaxMode)
	wipe(http.SameSiteNoneMode)

	// Also clear helper cookies
	for _, name := range []string{"oauth_state", "post_login_redirect"} {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/",
			HttpOnly: name != "oauth_state",
			Secure:   secure,
			SameSite: http.SameSiteLaxMode,
			Expires:  expired,
			MaxAge:   -1,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func (h *Handler) meHandler(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.parseSession(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(sess)
}

func (h *Handler) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if _, ok := h.parseSession(r); !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) parseSession(r *http.Request) (*Session, bool) {
	if h == nil || h.Auth == nil || h.Auth.CookieName == "" {
		return nil, false
	}
	c, err := r.Cookie(h.Auth.CookieName)
	if err != nil {
		return nil, false
	}
	parts := strings.Split(c.Value, ".")
	if len(parts) != 2 {
		return nil, false
	}
	mac := hmac.New(sha256.New, h.Auth.CookieKey)
	mac.Write([]byte(parts[0]))
	if base64.RawURLEncoding.EncodeToString(mac.Sum(nil)) != parts[1] {
		return nil, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, false
	}
	var s Session
	if json.Unmarshal(raw, &s) != nil || time.Now().After(s.Exp) {
		return nil, false
	}
	return &s, true
}
