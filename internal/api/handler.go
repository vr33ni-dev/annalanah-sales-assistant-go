// handler.go — Handler struct, constructor, and shared request helpers (writeJSONError, parseIDFromURL).
package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/auth"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/store"
)

type Handler struct {
	store appStore
	Cfg   *Config
	Auth  *auth.Auth
}

func NewHandler(s *store.PostgresStore, cfg *Config, a *auth.Auth) *Handler {
	return &Handler{store: s, Cfg: cfg, Auth: a}
}

// --- Helpers used by multiple handlers ---
func writeJSONError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func parseIDFromURL(path string) (int, bool) {
	parts := strings.Split(path, "/")
	id, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		return 0, false
	}
	return id, true
}
