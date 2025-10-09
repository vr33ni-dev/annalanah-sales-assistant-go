// api/helper.go
package api

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

/* ------------ Public JSON types ------------ */

type AppSetting struct {
	Key          string   `json:"key"`
	ValueNumeric *float64 `json:"value_numeric,omitempty"`
	ValueText    *string  `json:"value_text,omitempty"`
	UpdatedAt    *string  `json:"updated_at,omitempty"`
}

// GET /api/settings  -> list all settings
func (h *Handler) ListSettings(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.Query(`
		SELECT key,
		       value_numeric,
		       value_text,
		       to_char(updated_at, 'YYYY-MM-DD"T"HH24:MI:SSZ')
		FROM app_settings
		ORDER BY key`)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	var out []AppSetting
	for rows.Next() {
		var s AppSetting
		var vn sql.NullFloat64
		var vt sql.NullString
		var ua sql.NullString
		if err := rows.Scan(&s.Key, &vn, &vt, &ua); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if vn.Valid {
			s.ValueNumeric = &vn.Float64
		}
		if vt.Valid {
			s.ValueText = &vt.String
		}
		if ua.Valid {
			s.UpdatedAt = &ua.String
		}
		out = append(out, s)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// GET /api/settings/{key}
func (h *Handler) GetSetting(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")

	var vn sql.NullFloat64
	var vt sql.NullString
	var ua sql.NullString
	err := h.DB.QueryRow(`
		SELECT value_numeric,
		       value_text,
		       to_char(updated_at, 'YYYY-MM-DD"T"HH24:MI:SSZ')
		FROM app_settings
		WHERE key = $1`, key).Scan(&vn, &vt, &ua)

	if err == sql.ErrNoRows {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	resp := AppSetting{Key: key}
	if vn.Valid {
		resp.ValueNumeric = &vn.Float64
	}
	if vt.Valid {
		resp.ValueText = &vt.String
	}
	if ua.Valid {
		resp.UpdatedAt = &ua.String
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// PUT /api/settings/{key}
// Body can be either:
//
//	{ "value_numeric": 6 }
//
// or
//
//	{ "value_text": "something" }
func (h *Handler) UpsertSetting(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")

	var in AppSetting
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if in.ValueNumeric == nil && in.ValueText == nil {
		http.Error(w, "provide value_numeric or value_text", http.StatusBadRequest)
		return
	}

	_, err := h.DB.Exec(`
		INSERT INTO app_settings (key, value_numeric, value_text, updated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (key) DO UPDATE
		SET value_numeric = EXCLUDED.value_numeric,
		    value_text    = EXCLUDED.value_text,
		    updated_at    = now()
	`, key, in.ValueNumeric, in.ValueText)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	// Return the updated row
	h.GetSetting(w, r)
}

/* ------------ Internal helpers ------------ */

// getNumericSetting returns app_settings.value_numeric for a key,
// or the provided default if the key is missing/NULL.
func (h *Handler) getNumericSetting(key string, def float64) float64 {
	var v sql.NullFloat64
	_ = h.DB.QueryRow(`SELECT value_numeric FROM app_settings WHERE key = $1`, key).Scan(&v)
	if v.Valid {
		return v.Float64
	}
	return def
}
