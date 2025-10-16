package api

import (
	"encoding/json"
	"net/http"
)

func (h *Handler) health(w http.ResponseWriter, _ *http.Request) {
	// Lightweight readiness/liveness check
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":     true,
		"env":    h.Cfg.AppEnv,
		"status": "healthy",
	})
}
