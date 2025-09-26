package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

type Stage struct {
	ID            int      `json:"id"`
	Name          string   `json:"name"`
	Date          *string  `json:"date,omitempty"`
	AdBudget      *float64 `json:"ad_budget,omitempty"`
	Registrations *int     `json:"registrations,omitempty"`
	Participants  *int     `json:"participants,omitempty"`
}

// GET /api/stages
func (h *Handler) ListStages(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.Query(`
		SELECT id, name, date, ad_budget, registrations, participants
		FROM stages`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var stages []Stage
	for rows.Next() {
		var s Stage
		if err := rows.Scan(&s.ID, &s.Name, &s.Date, &s.AdBudget, &s.Registrations, &s.Participants); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		stages = append(stages, s)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stages)
}

// POST /api/stages
func (h *Handler) CreateStage(w http.ResponseWriter, r *http.Request) {
	var s Stage
	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	err := h.DB.QueryRow(
		`INSERT INTO stages (name, date, ad_budget, registrations, participants)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		s.Name, s.Date, s.AdBudget, s.Registrations, s.Participants,
	).Scan(&s.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s)
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
func (h *Handler) AddStageParticipant(w http.ResponseWriter, r *http.Request) {
	stageIDStr := chi.URLParam(r, "id")
	stageID, err := strconv.Atoi(stageIDStr)
	if err != nil {
		http.Error(w, "invalid stage id", http.StatusBadRequest)
		return
	}

	var req struct {
		ClientID  *int    `json:"client_id,omitempty"`
		LeadName  *string `json:"lead_name,omitempty"`
		LeadEmail *string `json:"lead_email,omitempty"`
		LeadPhone *string `json:"lead_phone,omitempty"`
		Attended  bool    `json:"attended"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// einfache Validierung: entweder client_id ODER lead_name muss vorhanden sein
	if req.ClientID == nil && (req.LeadName == nil || *req.LeadName == "") {
		http.Error(w, "either client_id or lead_name is required", http.StatusBadRequest)
		return
	}

	var insertErr error
	if req.ClientID != nil {
		_, insertErr = h.DB.Exec(
			`INSERT INTO stage_participants (stage_id, linked_client_id, attended)
			 VALUES ($1, $2, $3)`,
			stageID, *req.ClientID, req.Attended,
		)
	} else {
		_, insertErr = h.DB.Exec(
			`INSERT INTO stage_participants (stage_id, lead_name, lead_email, lead_phone, attended)
			 VALUES ($1, $2, $3, $4, $5)`,
			stageID, req.LeadName, req.LeadEmail, req.LeadPhone, req.Attended,
		)
	}
	if insertErr != nil {
		http.Error(w, insertErr.Error(), http.StatusInternalServerError)
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
		Attended *bool `json:"attended,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	_, err = h.DB.Exec(`
		UPDATE stage_participants
		SET attended = COALESCE($1, attended)
		WHERE id = $2 AND stage_id = $3`,
		req.Attended, participantID, stageID,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// PATCH /api/stages/{id}/stats
// Update aggregated numbers like registrations and participants count
func (h *Handler) UpdateStageStats(w http.ResponseWriter, r *http.Request) {
	stageIDStr := chi.URLParam(r, "id")
	stageID, err := strconv.Atoi(stageIDStr)
	if err != nil {
		http.Error(w, "invalid stage id", http.StatusBadRequest)
		return
	}

	var req struct {
		Registrations *int `json:"registrations,omitempty"`
		Participants  *int `json:"participants,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	_, err = h.DB.Exec(`
		UPDATE stages
		SET registrations = COALESCE($1, registrations),
		    participants  = COALESCE($2, participants)
		WHERE id = $3`,
		req.Registrations, req.Participants, stageID,
	)
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

	_, err = h.DB.Exec(
		`INSERT INTO stage_client_assignments (client_id, stage_id) VALUES ($1, $2)`,
		req.ClientID, stageID,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
}
