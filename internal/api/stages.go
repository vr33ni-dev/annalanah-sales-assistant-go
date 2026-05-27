package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/domain"
)

// GET /api/stages
// Monetary fields in response are Brutto/raw DB values.
func (h *Handler) ListStages(w http.ResponseWriter, r *http.Request) {
	stages, err := h.store.ListStages()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stages)

}

// GET /api/stages/{id}/participants
func (h *Handler) ListStageParticipants(w http.ResponseWriter, r *http.Request) {
	stageID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid stage id", http.StatusBadRequest)
		return
	}

	limit := 25
	offset := 0

	out, err := h.store.ListStageParticipants(stageID, limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// POST /api/stages
func (h *Handler) CreateStage(w http.ResponseWriter, r *http.Request) {
	var s struct {
		Name          string   `json:"name"`
		Date          *string  `json:"date,omitempty"`
		AdBudget      *float64 `json:"ad_budget,omitempty"`
		Registrations *int     `json:"registrations,omitempty"`
		Participants  *int     `json:"participants,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	stage := domain.Stage{
		Name:          s.Name,
		Date:          s.Date,
		AdBudget:      s.AdBudget,
		Registrations: s.Registrations,
		Participants:  s.Participants,
	}

	created, err := h.store.CreateStage(stage)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]int{"id": created.ID})
}

// DELETE /api/stages/{id}
func (h *Handler) DeleteStage(w http.ResponseWriter, r *http.Request) {
	stageID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid stage id", http.StatusBadRequest)
		return
	}
	err = h.store.DeleteStage(stageID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

/*
	POST /api/stages/{id}/participants

Request-Body (Lead ohne Client-ID):

	{
	  "lead_name": "Laura Beispiel",
	  "lead_email": "laura@example.com",
	  "lead_phone": "01234 5678",
	  "attended": true
	}

Request-Body (bestehender Client):

	{
		"client_id": 42,
		"attended": false
	}
*/
type AddStageParticipantRequest struct {
	ParticipantName  string  `json:"participant_name"`
	ParticipantEmail *string `json:"participant_email"`
	ParticipantPhone *string `json:"participant_phone"`

	LinkedClientID *int `json:"linked_client_id"`
	LinkedLeadID   *int `json:"linked_lead_id"`

	Attended     *bool `json:"attended,omitempty"`
	CreateAsLead bool  `json:"create_as_lead"`
}

func (h *Handler) AddStageParticipant(w http.ResponseWriter, r *http.Request) {
	stageID, _ := strconv.Atoi(chi.URLParam(r, "id"))

	// Flexible JSON decoding: accept both legacy keys (lead_name, lead_email,
	// lead_phone, client_id) and the newer participant_* / linked_* names.
	var raw map[string]any
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var req AddStageParticipantRequest

	// helper to read string
	getStr := func(keys ...string) *string {
		for _, k := range keys {
			if v, ok := raw[k]; ok && v != nil {
				if s, ok := v.(string); ok {
					s = strings.TrimSpace(s)
					return &s
				}
			}
		}
		return nil
	}

	// helper to read int
	getInt := func(keys ...string) *int {
		for _, k := range keys {
			if v, ok := raw[k]; ok && v != nil {
				switch t := v.(type) {
				case float64:
					iv := int(t)
					return &iv
				case int:
					iv := t
					return &iv
				}
			}
		}
		return nil
	}

	// helper to read bool pointer
	getBoolPtr := func(key string) *bool {
		if v, ok := raw[key]; ok && v != nil {
			if b, ok := v.(bool); ok {
				return &b
			}
		}
		return nil
	}

	req.ParticipantName = func() string {
		if s := getStr("participant_name", "lead_name"); s != nil {
			return *s
		}
		return ""
	}()
	req.ParticipantEmail = getStr("participant_email", "lead_email")
	req.ParticipantPhone = getStr("participant_phone", "lead_phone")

	req.LinkedClientID = getInt("linked_client_id", "client_id")
	req.LinkedLeadID = getInt("linked_lead_id")

	// attended/create_as_lead
	req.Attended = getBoolPtr("attended")
	if v := getBoolPtr("create_as_lead"); v != nil {
		req.CreateAsLead = *v
	}

	// Validation: require a name only if neither client nor lead is linked.
	if req.LinkedClientID == nil && req.LinkedLeadID == nil && req.ParticipantName == "" {
		http.Error(w, "client_id or participant_name required", http.StatusBadRequest)
		return
	}

	// If creating a lead, require email
	if req.CreateAsLead {
		if req.ParticipantEmail == nil || *req.ParticipantEmail == "" {
			http.Error(w, "participant_email required when create_as_lead is true", http.StatusBadRequest)
			return
		}
	}

	// lead creation is optional; we don't link to leads in stage_participants for sqlite tests

	// Only create an actual lead row when explicitly requested.
	if req.CreateAsLead {
		email := ""
		phone := ""
		if req.ParticipantEmail != nil {
			email = *req.ParticipantEmail
		}
		if req.ParticipantPhone != nil {
			phone = *req.ParticipantPhone
		}

		leadID, err := h.store.InsertLeadForStage(req.ParticipantName, email, phone, stageID)
		if err != nil {
			log.Printf("AddStageParticipant: failed creating lead: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		req.LinkedLeadID = &leadID
	}

	participant := domain.StageParticipant{
		ParticipantName:  req.ParticipantName,
		ParticipantEmail: req.ParticipantEmail,
		ParticipantPhone: req.ParticipantPhone,
		LinkedClientID:   req.LinkedClientID,
		LinkedLeadID:     req.LinkedLeadID,
		Attended:         req.Attended,
	}

	if _, err := h.store.AddStageParticipant(stageID, participant); err != nil {
		log.Printf("AddStageParticipant: insert failed: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

// PATCH /api/stages/{id}/participants/{participant_id}
// Update a single participant (e.g., mark attended after event)
func (h *Handler) UpdateStageParticipant(w http.ResponseWriter, r *http.Request) {
	stageIDStr := chi.URLParam(r, "id")
	participantIDStr := chi.URLParam(r, "participant_id")

	stageID, err := strconv.Atoi(stageIDStr)
	if err != nil {
		http.Error(w, "invalid stage id", http.StatusBadRequest)
		return
	}
	participantID, err := strconv.Atoi(participantIDStr)
	if err != nil {
		http.Error(w, "invalid participant id", http.StatusBadRequest)
		return
	}

	var req struct {
		StageID       int   `json:"stage_id"`       // optional, must match URL if provided
		ParticipantID int   `json:"participant_id"` // optional, must match URL if provided
		Attended      *bool `json:"attended,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	err = h.store.UpdateStageParticipant(stageID, domain.StageParticipant{
		ID:       participantID,
		Attended: req.Attended,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/stages/{id}/participants/{participant_id}
func (h *Handler) DeleteStageParticipant(w http.ResponseWriter, r *http.Request) {
	stageID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid stage id", http.StatusBadRequest)
		return
	}

	participantID, err := strconv.Atoi(chi.URLParam(r, "participant_id"))
	if err != nil {
		http.Error(w, "invalid participant id", http.StatusBadRequest)
		return
	}
	err = h.store.DeleteStageParticipant(stageID, participantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// PATCH /api/stages/{id}/stats
// Update aggregated numbers like registrations and participants count
func (h *Handler) UpdateStageStats(w http.ResponseWriter, r *http.Request) {
	stageID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid stage id", http.StatusBadRequest)
		return
	}

	type StageStats struct {
		Registrations *int `json:"registrations,omitempty"`
		Participants  *int `json:"participants,omitempty"`
	}
	var req StageStats

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	err = h.store.UpdateStageStats(stageID, req.Registrations, req.Participants)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// PATCH /api/stages/{id}
// Update base stage info like name, date, ad_budget
func (h *Handler) UpdateStageInfo(w http.ResponseWriter, r *http.Request) {
	stageIDStr := chi.URLParam(r, "id")
	stageID, err := strconv.Atoi(stageIDStr)
	if err != nil {
		http.Error(w, "invalid stage id", http.StatusBadRequest)
		return
	}

	var req struct {
		Name     *string  `json:"name,omitempty"`
		Date     *string  `json:"date,omitempty"`
		AdBudget *float64 `json:"ad_budget,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.Date != nil && *req.Date == "" {
		req.Date = nil
	}
	if req.Name != nil && *req.Name == "" {
		req.Name = nil
	}

	err = h.store.UpdateStageInfo(stageID, req.Name, req.Date, req.AdBudget)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// POST /api/stages/{id}/assign-client
func (h *Handler) AssignClientToStage(w http.ResponseWriter, r *http.Request) {
	stageIDStr := chi.URLParam(r, "id")
	stageID, err := strconv.Atoi(stageIDStr)
	if err != nil {
		http.Error(w, "invalid stage id", http.StatusBadRequest)
		return
	}

	var req struct {
		ClientID int `json:"client_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ClientID == 0 {
		http.Error(w, "client_id required", http.StatusBadRequest)
		return
	}

	err = h.store.AssignClientToStage(stageID, req.ClientID)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
}
