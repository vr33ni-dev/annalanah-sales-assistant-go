// api/helper.go
package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"math"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/lib/pq"

	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/domain"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/store"
)

func netFromGross(gross, mwstRate float64) float64 {
	if mwstRate <= 0 {
		return gross
	}
	return gross / mwstRate
}

func nullStringToPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	s := ns.String
	return &s
}

func nullTimeToString(nt sql.NullTime, layout string) *string {
	if !nt.Valid {
		return nil
	}
	s := nt.Time.Format(layout)
	return &s
}

// GET /api/settings
func (h *Handler) ListSettings(w http.ResponseWriter, r *http.Request) {
	out, err := h.store.ListSettings()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if out == nil {
		out = []domain.AppSetting{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// GET /api/settings/{key}
func (h *Handler) GetSetting(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")

	setting, err := h.store.GetSetting(key)
	if err == store.ErrNotFound {
		if key == "new_contract_notify_email" {
			resp := domain.AppSetting{Key: key}
			fallback := strings.TrimSpace(os.Getenv("NEW_CONTRACT_NOTIFY_EMAIL"))
			if fallback != "" {
				resp.ValueText = &fallback
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		log.Printf("GetSetting: key=%q query failed: %v", key, err)
		http.Error(w, err.Error(), 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(setting)
}

// PUT /api/settings/{key}
// Body: { "value_numeric": 6 } or { "value_text": "something" }
func (h *Handler) UpsertSetting(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")

	var in domain.AppSetting
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if in.ValueNumeric == nil && in.ValueText == nil {
		http.Error(w, "provide value_numeric or value_text", http.StatusBadRequest)
		return
	}

	if key == "potential_months" {
		if in.ValueNumeric == nil {
			http.Error(w, "potential_months requires value_numeric", http.StatusBadRequest)
			return
		}
		v := *in.ValueNumeric
		if v <= 0 {
			http.Error(w, "potential_months must be > 0", http.StatusBadRequest)
			return
		}
		if v != math.Trunc(v) {
			http.Error(w, "potential_months must be an integer", http.StatusBadRequest)
			return
		}
	}

	if err := h.store.UpsertSetting(key, in.ValueNumeric, in.ValueText); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	h.GetSetting(w, r)
}

/* ------------ Internal helpers ------------ */

func (h *Handler) getNumericSetting(key string, def float64) float64 {
	return h.store.GetNumericSetting(key, def)
}

func (h *Handler) getTextSetting(key string, def string) string {
	return h.store.GetTextSetting(key, def)
}

func isUniqueViolation(err error, constraint string) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505" && pqErr.Constraint == constraint
}
