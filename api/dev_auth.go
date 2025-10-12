package api

import (
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/utils"
)

func (h *Handler) MountDevRoutes(r chi.Router) {
	if !utils.IsLocalEnv() {
		return
	}
	r.Get("/dev/login-as", h.devLoginAs)
}

func (h *Handler) devLoginAs(w http.ResponseWriter, r *http.Request) {
	// 1) read query
	email := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("email")))
	if email == "" {
		http.Error(w, "email required", http.StatusBadRequest)
		return
	}
	name := r.URL.Query().Get("name")
	if name == "" {
		name = email
	}

	// 2) enforce allowlist (same as your Google flow)
	if !h.Auth.Allowed[email] {
		http.Error(w, "not allowed", http.StatusForbidden)
		return
	}

	// 3) cookie settings like your callback
	domain := os.Getenv("COOKIE_DOMAIN") // empty in dev
	sameSite := http.SameSiteLaxMode
	if domain != "" {
		sameSite = http.SameSiteNoneMode
	}

	// 4) issue the exact same session cookie your app uses
	ck := h.Auth.makeCookie(Session{
		Email: email,
		Name:  name,
		Exp:   time.Now().Add(12 * time.Hour),
	}, domain, sameSite)
	http.SetCookie(w, ck)
	w.WriteHeader(http.StatusNoContent)
}
