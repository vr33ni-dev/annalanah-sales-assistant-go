// leads.go — HTTP handlers for leads: list, create, update, delete, convert to client.
package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

type LeadResponse struct {
	ID                int     `json:"id"`
	Name              string  `json:"name"`
	Email             string  `json:"email"`
	Phone             string  `json:"phone"`
	Source            string  `json:"source"`
	SourceStageID     *int    `json:"source_stage_id,omitempty"`
	SourceStageName   *string `json:"source_stage_name,omitempty"`
	Converted         bool    `json:"converted"`
	ConvertedClientID *int    `json:"converted_client_id,omitempty"`
	CreatedAt         *string `json:"created_at,omitempty"`
}

// GET /api/leads
func (h *Handler) ListLeads(w http.ResponseWriter, r *http.Request) {
	leads, err := h.store.ListLeads(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	out := make([]LeadResponse, len(leads))
	for i, l := range leads {
		out[i] = LeadResponse{
			ID:                l.ID,
			Name:              l.Name,
			Email:             l.Email,
			Phone:             l.Phone,
			Source:            l.Source,
			SourceStageID:     l.SourceStageID,
			SourceStageName:   l.SourceStageName,
			Converted:         l.Converted,
			ConvertedClientID: l.ConvertedClientID,
			CreatedAt:         l.CreatedAt,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// POST /api/leads
func (h *Handler) CreateLead(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Name            string  `json:"name"`
		Email           *string `json:"email"`
		Phone           *string `json:"phone"`
		Source          string  `json:"source"`
		SourceStageID   *int    `json:"source_stage_id,omitempty"`
		SourceStageName *string `json:"source_stage_name,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	if payload.Email != nil {
		lower := strings.ToLower(*payload.Email)
		payload.Email = &lower
	}

	lead, isExisting, err := h.store.CreateLead(r.Context(), payload.Name, payload.Email, payload.Phone, payload.Source, payload.SourceStageID)
	if err != nil {
		http.Error(w, "failed creating lead: "+err.Error(), http.StatusInternalServerError)
		return
	}

	out := LeadResponse{
		ID:                lead.ID,
		Name:              lead.Name,
		Email:             lead.Email,
		Phone:             lead.Phone,
		Source:            lead.Source,
		SourceStageID:     lead.SourceStageID,
		SourceStageName:   lead.SourceStageName,
		Converted:         lead.Converted,
		ConvertedClientID: lead.ConvertedClientID,
		CreatedAt:         lead.CreatedAt,
	}

	if isExisting {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
	} else {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
	}
	_ = json.NewEncoder(w).Encode(out)
}

// PATCH /api/leads/{id}
func (h *Handler) UpdateLead(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	leadID, err := strconv.Atoi(idStr)
	if err != nil || leadID <= 0 {
		http.Error(w, "invalid lead id", http.StatusBadRequest)
		return
	}

	var payload struct {
		Name            *string `json:"name"`
		Email           *string `json:"email"`
		Phone           *string `json:"phone"`
		Source          *string `json:"source"`
		SourceStageID   *int    `json:"source_stage_id,omitempty"`
		SourceStageName *string `json:"source_stage_name,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	if payload.Name == nil && payload.Email == nil && payload.Phone == nil && payload.Source == nil && payload.SourceStageID == nil && payload.SourceStageName == nil {
		http.Error(w, "no fields to update", http.StatusBadRequest)
		return
	}

	lead, err := h.store.UpdateLead(r.Context(), leadID, payload.Name, payload.Email, payload.Phone, payload.Source, payload.SourceStageID)
	if err != nil {
		if err.Error() == "lead not found" {
			http.Error(w, "lead not found", http.StatusNotFound)
			return
		}
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	out := LeadResponse{
		ID:                lead.ID,
		Name:              lead.Name,
		Email:             lead.Email,
		Phone:             lead.Phone,
		Source:            lead.Source,
		SourceStageID:     lead.SourceStageID,
		SourceStageName:   lead.SourceStageName,
		Converted:         lead.Converted,
		ConvertedClientID: lead.ConvertedClientID,
		CreatedAt:         lead.CreatedAt,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// DELETE /api/leads/{id}
func (h *Handler) DeleteLead(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	leadID, err := strconv.Atoi(idStr)
	if err != nil || leadID <= 0 {
		http.Error(w, "invalid lead id", http.StatusBadRequest)
		return
	}

	if err := h.store.DeleteLead(r.Context(), leadID); err != nil {
		if err.Error() == "lead not found" {
			http.Error(w, "lead not found", http.StatusNotFound)
			return
		}
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// POST /api/leads/{id}/convert
func (h *Handler) ConvertLead(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	leadID, err := strconv.Atoi(idStr)
	if err != nil || leadID <= 0 {
		http.Error(w, "invalid lead id", http.StatusBadRequest)
		return
	}

	clientID, salesProcessID, err := h.store.ConvertLead(r.Context(), leadID)
	if err != nil {
		if err.Error() == "lead not found" {
			http.Error(w, "lead not found", http.StatusNotFound)
			return
		}
		http.Error(w, "conversion failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]int{"client_id": clientID, "sales_process_id": salesProcessID})
}
