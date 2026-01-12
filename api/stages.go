package api

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

type Stage struct {
	ID               int      `json:"id"`
	Name             string   `json:"name"`
	Date             *string  `json:"date,omitempty"`
	AdBudget         *float64 `json:"ad_budget,omitempty"`
	Registrations    *int     `json:"registrations,omitempty"`
	Participants     *int     `json:"participants,omitempty"`      // manual
	RecordedContacts *int     `json:"recorded_contacts,omitempty"` // derived
}

// GET /api/stages
func (h *Handler) ListStages(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.Query(`
SELECT
  s.id,
  s.name,
  s.date,
  s.ad_budget,
  s.registrations,
  s.participants,
  COUNT(sp.id) AS recorded_contacts
FROM stages s
LEFT JOIN stage_participants sp ON sp.stage_id = s.id
GROUP BY
  s.id,
  s.name,
  s.date,
  s.ad_budget,
  s.registrations,
  s.participants
ORDER BY s.id;
`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var stages []Stage
	for rows.Next() {
		var s Stage
		if err := rows.Scan(&s.ID, &s.Name, &s.Date, &s.AdBudget, &s.Registrations, &s.Participants, &s.RecordedContacts); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		stages = append(stages, s)
	}

	if stages == nil {
		stages = []Stage{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stages)
}

type StageParticipant struct {
	ID      int `json:"id"`
	StageID int `json:"stage_id"`

	LinkedClientID *int `json:"linked_client_id,omitempty"`
	LinkedLeadID   *int `json:"linked_lead_id,omitempty"`

	ParticipantName  string  `json:"name"`
	ParticipantEmail *string `json:"email,omitempty"`
	ParticipantPhone *string `json:"phone,omitempty"`

	Attended  *bool   `json:"attended,omitempty"`
	CreatedAt *string `json:"created_at,omitempty"`
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

	rows, err := h.DB.Query(`
				SELECT
					sp.id,
					sp.stage_id,
					sp.linked_client_id,
					sp.linked_lead_id,
					COALESCE(c.name, l.name, sp.participant_name),
					COALESCE(c.email, l.email, sp.participant_email),
					COALESCE(c.phone, l.phone, sp.participant_phone),
					sp.attended,
					sp.created_at
				FROM stage_participants sp
				LEFT JOIN clients c ON c.id = sp.linked_client_id
				LEFT JOIN leads   l ON l.id = sp.linked_lead_id
				WHERE sp.stage_id = $1
				ORDER BY sp.id
				LIMIT $2 OFFSET $3
		`, stageID, limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var out []StageParticipant
	for rows.Next() {
		var p StageParticipant
		var nb sql.NullBool
		if err := rows.Scan(
			&p.ID,
			&p.StageID,
			&p.LinkedClientID,
			&p.LinkedLeadID,
			&p.ParticipantName,
			&p.ParticipantEmail,
			&p.ParticipantPhone,
			&nb,
			&p.CreatedAt,
		); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if nb.Valid {
			b := nb.Bool
			p.Attended = &b
		} else {
			p.Attended = nil
		}
		out = append(out, p)
	}

	if out == nil {
		out = []StageParticipant{}
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

	var id int
	err := h.DB.QueryRow(`
		INSERT INTO stages (name, date, ad_budget, registrations, participants)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, s.Name, s.Date, s.AdBudget, s.Registrations, s.Participants).Scan(&id)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	// historical tests expect 200 OK here
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]int{"id": id})
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

	var req AddStageParticipantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Require a name for every participant. Email and phone are optional unless
	// a new lead is created - in that case we require an email so the lead has
	// contact information.
	if req.ParticipantName == "" {
		http.Error(w, "participant_name required", http.StatusBadRequest)
		return
	}

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

		row := h.DB.QueryRow(`
		INSERT INTO leads (name, email, phone, source, source_stage_id)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`,
			req.ParticipantName,
			email,
			phone,
			"paid",
			stageID,
		)

		var id int
		if err := row.Scan(&id); err != nil {
			log.Printf("AddStageParticipant: failed creating lead: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// link the newly created lead to the stage participant insert
		req.LinkedLeadID = &id
	}

	// Prepare arguments converting nil pointers to untyped nil so the driver
	// writes NULL into the DB when values are absent.
	var pEmail interface{} = nil
	var pPhone interface{} = nil
	var linkedClient interface{} = nil
	var linkedLead interface{} = nil
	if req.ParticipantEmail != nil {
		pEmail = *req.ParticipantEmail
	}
	if req.ParticipantPhone != nil {
		pPhone = *req.ParticipantPhone
	}
	if req.LinkedClientID != nil {
		linkedClient = *req.LinkedClientID
	}
	if req.LinkedLeadID != nil {
		linkedLead = *req.LinkedLeadID
	}

	var attended interface{} = nil
	if req.Attended != nil {
		attended = *req.Attended
	}

	args := []interface{}{stageID, req.ParticipantName, pEmail, pPhone, linkedClient, linkedLead, attended}

	// Log the args for debugging in dev — safe because these are non-secret values
	log.Printf("AddStageParticipant: inserting with args=%v", args)

	res, err := h.DB.Exec(`
		INSERT INTO stage_participants (
			stage_id,
			participant_name,
			participant_email,
			participant_phone,
			linked_client_id,
			linked_lead_id,
			attended
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
	`, args...)

	if err != nil {
		log.Printf("AddStageParticipant: insert into stage_participants failed: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Optionally log rows affected when available
	if ra, err2 := res.RowsAffected(); err2 == nil {
		log.Printf("AddStageParticipant: rows affected=%d", ra)
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

	res, err := h.DB.Exec(`
		DELETE FROM stage_participants
		WHERE id = $1 AND stage_id = $2
	`, participantID, stageID)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	rows, _ := res.RowsAffected()
	if rows == 0 {
		http.Error(w, "participant not found", http.StatusNotFound)
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
		    participants = COALESCE($2, participants)
		WHERE id = $3
	`, req.Registrations, req.Participants, stageID)

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

	_, err = h.DB.Exec(`
		UPDATE stages
		SET 
			name = COALESCE($1, name),
			date = COALESCE($2, date),
			ad_budget = COALESCE($3, ad_budget)
		WHERE id = $4
	`, req.Name, req.Date, req.AdBudget, stageID)

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
