// auth_handlers.go — HTTP handlers for authentication: login, logout, OAuth callback, session (/me).
package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/oauth2"
	"google.golang.org/api/idtoken"

	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/auth"
)

func (h *Handler) MountAuthRoutes(r chi.Router) {
	r.Get("/auth/google", h.handleAuthStart)
	r.Get("/auth/google/callback", h.handleAuthCallback)
	r.Get("/api/me", h.meHandler)
	r.Get("/api/user/me", h.userMeHandler)
	r.Get("/debug/session", h.debugSession)

	r.MethodFunc(http.MethodGet, "/auth/logout", h.handleLogout)
	r.MethodFunc(http.MethodPost, "/auth/logout", h.handleLogout)
}

func (h *Handler) handleAuthStart(w http.ResponseWriter, r *http.Request) {
	log.Printf("handleAuthStart: query redirect=%q remote=%s\n", r.URL.Query().Get("redirect"), r.RemoteAddr)

	state := auth.RandState()
	secure := auth.IsSecure(r)
	sameSite := http.SameSiteLaxMode

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

	http.Redirect(w, r, h.Auth.OAuth.AuthCodeURL(
		state,
		oauth2.AccessTypeOnline,
		oauth2.SetAuthURLParam("prompt", "select_account"),
	), http.StatusFound)
}

func (h *Handler) handleAuthCallback(w http.ResponseWriter, r *http.Request) {
	log.Printf("handleAuthCallback: host=%q xf-host=%q proto=%q rawQuery=%q remote=%s",
		r.Host, r.Header.Get("X-Forwarded-Host"), r.Header.Get("X-Forwarded-Proto"), r.URL.RawQuery, r.RemoteAddr)

	secure := auth.IsSecure(r)
	sameSite := http.SameSiteLaxMode

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

	ck := h.Auth.MakeCookie(auth.Session{
		Email: email,
		Name:  name,
		Exp:   time.Now().Add(12 * time.Hour),
	}, secure)
	http.SetCookie(w, ck)

	redirectTo := os.Getenv("POST_LOGIN_REDIRECT")
	if redirectTo == "" {
		redirectTo = "/"
	}
	if rc, err := r.Cookie("post_login_redirect"); err == nil && rc.Value != "" {
		redirectTo = rc.Value
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

	if strings.Contains(redirectTo, "?") {
		redirectTo = redirectTo + "&auth=signed_in"
	} else {
		redirectTo = redirectTo + "?auth=signed_in"
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(fmt.Sprintf(
		`<!doctype html>
<meta charset="utf-8">
<title>Signing you in…</title>
<meta http-equiv="refresh" content="0;url=%[1]s">
<script>
  // Double attempt in case a proxy caches the first load
  try { window.location.replace(%q); } catch (_) { window.location.href = %q; }
</script>`,
		redirectTo, redirectTo, redirectTo,
	)))
}

func (h *Handler) handleLogout(w http.ResponseWriter, r *http.Request) {
	expired := time.Unix(0, 0)
	secure := auth.IsSecure(r)

	for _, ss := range []http.SameSite{http.SameSiteLaxMode, http.SameSiteNoneMode} {
		http.SetCookie(w, &http.Cookie{
			Name:     h.Auth.CookieName,
			Value:    "",
			Path:     "/",
			HttpOnly: true,
			Secure:   secure || ss == http.SameSiteNoneMode,
			SameSite: ss,
			Expires:  expired,
			MaxAge:   -1,
		})
	}

	if dom := strings.TrimSpace(os.Getenv("COOKIE_DOMAIN")); dom != "" {
		http.SetCookie(w, &http.Cookie{
			Name:     h.Auth.CookieName,
			Value:    "",
			Path:     "/",
			Domain:   dom,
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteNoneMode,
			Expires:  expired,
			MaxAge:   -1,
		})
	}

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

	dest := strings.TrimSpace(os.Getenv("POST_LOGOUT_REDIRECT"))
	if dest == "" {
		dest = strings.TrimSpace(os.Getenv("POST_LOGIN_REDIRECT"))
	}
	if dest == "" {
		if o := strings.TrimSpace(r.Header.Get("Origin")); o != "" {
			dest = o
		} else if ref := strings.TrimSpace(r.Header.Get("Referer")); ref != "" {
			if u, err := url.Parse(ref); err == nil {
				u.Path, u.RawQuery, u.Fragment = "/", "", ""
				dest = u.String()
			}
		}
	}
	if dest == "" {
		dest = "/"
	}

	if strings.Contains(dest, "?") {
		dest = dest + "&auth=logged_out"
	} else {
		dest = dest + "?auth=logged_out"
	}

	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, dest, http.StatusFound)
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

func (h *Handler) debugSession(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.parseSession(r)
	w.Header().Set("Content-Type", "application/json")
	if !ok {
		var resp = map[string]interface{}{
			"ok":      false,
			"message": "no valid session found",
			"cookies": map[string]string{},
		}
		for _, ck := range r.Cookies() {
			resp["cookies"].(map[string]string)[ck.Name] = ck.Value
		}
		_ = json.NewEncoder(w).Encode(resp)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "session": sess})
}

func (h *Handler) userMeHandler(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.parseSession(r)
	w.Header().Set("Content-Type", "application/json")
	if ok && sess != nil {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"authenticated": true,
			"name":          sess.Name,
			"email":         sess.Email,
		})
		return
	}
	def := os.Getenv("DEFAULT_COMMENT_AUTHOR")
	if def == "" {
		def = "local-dev"
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"authenticated": false,
		"name":          def,
		"email":         "",
	})
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

func (h *Handler) parseSession(r *http.Request) (*auth.Session, bool) {
	if h == nil || h.Auth == nil {
		return nil, false
	}
	return h.Auth.ParseSession(r)
}
