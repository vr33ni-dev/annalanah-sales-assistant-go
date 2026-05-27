package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/domain"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/store"
)

// GET /api/clients
func (h *Handler) ListClients(w http.ResponseWriter, r *http.Request) {
	type ClientListResponse struct {
		ID              int64             `json:"id"`
		LeadID          *int64            `json:"lead_id,omitempty"`
		Name            string            `json:"name"`
		Email           string            `json:"email"`
		Phone           string            `json:"phone"`
		Source          string            `json:"source"`
		SourceStageName string            `json:"source_stage_name"`
		Status          string            `json:"status"`
		CompletedAt     *string           `json:"completed_at,omitempty"`
		Comments        []CommentResponse `json:"comments,omitempty"`
	}

	includeInactive := r.URL.Query().Get("include_inactive") == "true"

	domClients, err := h.store.ListClients(r.Context(), includeInactive)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	clients := make([]ClientListResponse, len(domClients))
	for i, dc := range domClients {
		comments := make([]CommentResponse, len(dc.Comments))
		for j, c := range dc.Comments {
			comments[j] = CommentResponse{
				ID:         c.ID,
				ClientID:   c.ClientID,
				EntityType: c.EntityType,
				EntityID:   c.EntityID,
				Author:     c.Author,
				Body:       c.Body,
				Metadata:   c.Metadata,
				CreatedAt:  c.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
				UpdatedAt:  c.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
			}
		}
		clients[i] = ClientListResponse{
			ID:              dc.ID,
			LeadID:          dc.LeadID,
			Name:            dc.Name,
			Email:           dc.Email,
			Phone:           dc.Phone,
			Source:          dc.Source,
			SourceStageName: dc.SourceStageName,
			Status:          dc.Status,
			CompletedAt:     dc.CompletedAt,
			Comments:        comments,
		}
	}

	activeCount := 0
	for _, c := range clients {
		if c.Status == "active" {
			activeCount++
		}
	}
	log.Printf("ListClients: returning %d clients (active=%d)", len(clients), activeCount)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(clients)
}

// POST /api/clients
func (h *Handler) CreateClient(w http.ResponseWriter, r *http.Request) {
	var c domain.Client
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		http.Error(w, "Ungültige Anfrage", http.StatusBadRequest)
		return
	}

	if c.Status == "" {
		http.Error(w, "status is required", http.StatusBadRequest)
		return
	}

	c.Email = strings.ToLower(c.Email)

	id, err := h.store.InsertClient(r.Context(), c.Name, c.Email, c.Phone, c.Source, c.SourceStageID, c.Status)
	if err != nil {
		if errors.Is(err, store.ErrDuplicateEmail) {
			writeJSONError(w, "Ein Kunde mit dieser E-Mail-Adresse existiert bereits.", http.StatusConflict)
			return
		}
		var pqErr *pq.Error
		if errors.As(err, &pqErr) {
			log.Printf("Postgres error: %s (code=%s, constraint=%s)", pqErr.Message, pqErr.Code, pqErr.Constraint)
			if pqErr.Code == "23505" {
				writeJSONError(w, "Doppelter Eintrag: "+pqErr.Constraint, http.StatusConflict)
				return
			}
		}
		writeJSONError(w, "Fehler beim Anlegen des Kunden.", http.StatusInternalServerError)
		return
	}
	c.ID = id

	if len(c.Comments) > 0 {
		if err := h.insertCommentsForEntity("client", c.ID, c.ID, c.Comments); err != nil {
			log.Printf("failed to insert comments for client %d: %v", c.ID, err)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(c)
}

// DELETE /api/clients/{id}
func (h *Handler) DeleteClient(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDFromURL(r.URL.Path)
	if !ok {
		http.Error(w, "invalid client ID", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	found, err := h.store.DeleteClientWithLeadReset(ctx, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !found {
		http.Error(w, "client not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// PATCH /api/clients/{id}
func (h *Handler) UpdateClient(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDFromURL(r.URL.Path)
	if !ok {
		http.Error(w, "invalid client ID", http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil || len(body) == 0 {
		http.Error(w, "empty or unreadable body", http.StatusBadRequest)
		return
	}
	log.Printf("PATCH /clients raw body: %s", string(body))

	// Unmarshal into a generic map for flexible types
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	// Unmarshal into an auxiliary struct so completed_at is a string
	var updated struct {
		Name          string           `json:"name"`
		Email         string           `json:"email"`
		Phone         string           `json:"phone"`
		Source        string           `json:"source"`
		SourceStageID *int             `json:"source_stage_id,omitempty"`
		Status        string           `json:"status"`
		CompletedAt   *string          `json:"completed_at,omitempty"`
		Comments      []domain.Comment `json:"comments,omitempty"`
	}
	if err := json.Unmarshal(body, &updated); err != nil {
		log.Printf("❌ decode error: %v", err)
		http.Error(w, "invalid client data", http.StatusBadRequest)
		return
	}

	updated.Email = strings.ToLower(updated.Email)

	// Parse completed_at string (if any)
	var completedAt *time.Time
	if updated.CompletedAt != nil && *updated.CompletedAt != "" {
		if t, err := time.Parse("2006-01-02", *updated.CompletedAt); err == nil {
			completedAt = &t
		} else {
			http.Error(w, "invalid completed_at, expected YYYY-MM-DD", http.StatusBadRequest)
			return
		}
	}

	// Only validate completed_at if the value is actually changing.
	// Frontends often echo back the existing value; re-validating it would incorrectly
	// reject seeded or historically-set dates that pre-date created_at.
	existing, _ := h.store.GetClientCompletedAt(r.Context(), id)
	existingDateStr := ""
	if existing != nil {
		existingDateStr = existing.Format("2006-01-02")
	}
	incomingDateStr := ""
	if updated.CompletedAt != nil {
		incomingDateStr = *updated.CompletedAt
	}
	if incomingDateStr != existingDateStr {
		if err := h.store.ValidateClientCompletedAt(r.Context(), id, completedAt); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	// Detect whether source_stage_id was explicitly provided (including explicit null = unlink).
	_, sourceStageIDProvided := raw["source_stage_id"]

	if err := h.store.UpdateClientFields(r.Context(), id, updated.Name, updated.Email, updated.Phone, updated.Source, updated.Status, updated.SourceStageID, sourceStageIDProvided, completedAt); err != nil {
		if errors.Is(err, store.ErrDuplicateEmail) {
			http.Error(w, "Ein Kunde mit dieser E-Mail-Adresse existiert bereits", http.StatusConflict)
			return
		}
		log.Printf("❌ update failed: %v", err)
		http.Error(w, "update failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// If source_stage_id was explicitly cleared, cascade: unlink all sales processes for this client.
	if sourceStageIDProvided && updated.SourceStageID == nil {
		if err := h.store.ClearClientSalesProcessStageID(r.Context(), id); err != nil {
			log.Printf("❌ cascade stage_id clear failed for client %d: %v", id, err)
		}
	}

	// Sync the same contact fields to the linked converted lead (if any).
	if err := h.store.SyncLeadFromClient(r.Context(), id, updated.Name, updated.Email, updated.Phone, updated.Source, updated.SourceStageID, sourceStageIDProvided); err != nil {
		log.Printf("❌ lead sync failed for client %d: %v", id, err)
	}

	if len(updated.Comments) > 0 {
		if err := h.insertCommentsForEntity("client", id, id, updated.Comments); err != nil {
			log.Printf("failed to insert comments for client %d: %v", id, err)
		}
	}

	w.WriteHeader(http.StatusNoContent)
}
