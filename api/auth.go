// file: api/auth.go
package api

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
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
			RedirectURL:  os.Getenv("OAUTH_REDIRECT_URL"),
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

func (a *Auth) makeCookie(sess Session, domain string, sameSite http.SameSite) *http.Cookie {
	payload, _ := json.Marshal(sess)
	enc := base64.RawURLEncoding.EncodeToString(payload)
	token := enc + "." + a.sign([]byte(enc))
	return &http.Cookie{
		Name:     a.CookieName,
		Value:    token,
		Path:     "/",
		Domain:   domain,
		HttpOnly: true,
		Secure:   sameSite == http.SameSiteNoneMode || strings.HasPrefix(os.Getenv("POST_LOGIN_REDIRECT"), "https://"),
		SameSite: sameSite,
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
	r.Post("/auth/logout", h.handleLogout)
	r.Get("/api/me", h.meHandler)
}

// --- Handlers ---

func (h *Handler) handleAuthStart(w http.ResponseWriter, r *http.Request) {
	log.Printf("handleAuthStart: query redirect=%q remote=%s\n", r.URL.Query().Get("redirect"), r.RemoteAddr)

	state := randState()

	// decide Secure / SameSite for prod cross-site flows
	secure := strings.HasPrefix(os.Getenv("POST_LOGIN_REDIRECT"), "https://") || os.Getenv("APP_ENV") == "prod"
	sameSite := http.SameSiteLaxMode
	if secure {
		sameSite = http.SameSiteNoneMode
	}

	// oauth_state cookie (not HttpOnly so browser sends it back on navigation)
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    state,
		Path:     "/",
		HttpOnly: false,
		Secure:   secure,
		SameSite: sameSite,
		Expires:  time.Now().Add(10 * time.Minute),
	})

	// store optional post-login redirect (frontend sends ?redirect=...)
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
	log.Printf("handleAuthCallback: rawQuery=%q remote=%s\n", r.URL.RawQuery, r.RemoteAddr)

	// CSRF check
	state := r.URL.Query().Get("state")
	stateC, _ := r.Cookie("oauth_state")
	if state == "" || stateC == nil || state != stateC.Value {
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}

	code := r.URL.Query().Get("code")
	tok, err := h.Auth.OAuth.Exchange(r.Context(), code)
	if err != nil {
		http.Error(w, "exchange failed", http.StatusUnauthorized)
		return
	}

	// Verify Google ID token signature & audience
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

	// Cookie settings per env
	domain := os.Getenv("COOKIE_DOMAIN") // e.g. ".yourapp.com" in prod, empty in dev
	sameSite := http.SameSiteLaxMode
	if domain != "" || strings.HasPrefix(os.Getenv("POST_LOGIN_REDIRECT"), "https://") || os.Getenv("APP_ENV") == "prod" {
		sameSite = http.SameSiteNoneMode
	}

	ck := h.Auth.makeCookie(Session{
		Email: email,
		Name:  name,
		Exp:   time.Now().Add(12 * time.Hour),
	}, domain, sameSite)
	// ensure Secure when SameSite=None
	if ck.SameSite == http.SameSiteNoneMode {
		ck.Secure = true
	}

	log.Printf("callback Set-Cookie domain=%q sameSite=%v secure=%v exp=%v",
		ck.Domain, ck.SameSite, ck.Secure, ck.Expires)

	ck.Partitioned = true
	http.SetCookie(w, ck) // true in dev/prod on Render

	// prefer post_login_redirect cookie set in handleAuthStart, fallback to env
	redirectTo := os.Getenv("POST_LOGIN_REDIRECT")
	if redirectTo == "" {
		redirectTo = "/"
	}
	if rc, err := r.Cookie("post_login_redirect"); err == nil && rc.Value != "" {
		redirectTo = rc.Value
		// clear the cookie
		http.SetCookie(w, &http.Cookie{
			Name:     "post_login_redirect",
			Value:    "",
			Path:     "/",
			Domain:   domain,
			HttpOnly: true,
			Secure:   sameSite == http.SameSiteNoneMode || strings.HasPrefix(os.Getenv("POST_LOGIN_REDIRECT"), "https://"),
			SameSite: sameSite,
			Expires:  time.Unix(0, 0),
		})
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Optional, but sometimes helps caches/proxies
	w.Header().Set("Cache-Control", "no-store")
	page := fmt.Sprintf(`<!doctype html>
<meta http-equiv="refresh" content="0;url=%[1]s">
<script>window.location.assign(%q)</script>
`, redirectTo, redirectTo)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(page))
}

func (h *Handler) handleLogout(w http.ResponseWriter, r *http.Request) {
	domain := os.Getenv("COOKIE_DOMAIN") // usually empty on Render

	// emit multiple Set-Cookie variants to cover:
	// - with domain / without domain
	// - SameSite=None (Secure) / SameSite=Lax
	// - Secure true/false (browsers ignore for deletion identity, but we cover it)
	expired := time.Unix(0, 0)

	writeClears := func(withDomain bool, sameSite http.SameSite, secure bool) {
		base := &http.Cookie{
			Name:     h.Auth.CookieName,
			Value:    "",
			Path:     "/",
			HttpOnly: true,
			Secure:   secure,
			SameSite: sameSite,
			Expires:  expired,
			MaxAge:   -1,
		}
		if withDomain && domain != "" {
			base.Domain = domain
		}
		http.SetCookie(w, base)
	}

	// clear app_session in all common permutations
	for _, withDomain := range []bool{false} {
		for _, sameSite := range []http.SameSite{http.SameSiteLaxMode, http.SameSiteNoneMode} {
			for _, secure := range []bool{false} {
				// ensure Secure when SameSite=None to satisfy browsers
				s := secure
				if sameSite == http.SameSiteNoneMode {
					s = true
				}
				writeClears(withDomain, sameSite, s)
			}
		}
	}

	// Optional: clear helper cookies set during auth
	for _, name := range []string{"oauth_state", "post_login_redirect"} {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/",
			Expires:  expired,
			MaxAge:   -1,
			HttpOnly: name != "oauth_state", // oauth_state was not HttpOnly originally
			Secure:   true,
			SameSite: http.SameSiteNoneMode,
			Domain:   domain,
		})
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/",
			Expires:  expired,
			MaxAge:   -1,
			HttpOnly: name != "oauth_state",
		})
	}

	// Return 200 JSON so some stacks don't drop Set-Cookie on 204
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

// Middleware usable for protected routes.
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

	var candidates []*http.Cookie
	for _, ck := range r.Cookies() {
		if ck.Name == h.Auth.CookieName {
			candidates = append(candidates, ck)
		}
	}
	if len(candidates) == 0 {
		return nil, false
	}

	// try each candidate until one validates
	for _, c := range candidates {
		parts := strings.Split(c.Value, ".")
		if len(parts) != 2 {
			continue
		}
		mac := hmac.New(sha256.New, h.Auth.CookieKey)
		mac.Write([]byte(parts[0]))
		if base64.RawURLEncoding.EncodeToString(mac.Sum(nil)) != parts[1] {
			continue // signature mismatch → try next cookie
		}
		raw, err := base64.RawURLEncoding.DecodeString(parts[0])
		if err != nil {
			continue
		}
		var s Session
		if json.Unmarshal(raw, &s) != nil || time.Now().After(s.Exp) {
			continue
		}
		// this one is valid
		return &s, true
	}

	// optional: log once to help diagnose
	log.Printf("parseSession: %d app_session cookies but none valid", len(candidates))
	return nil, false
}
