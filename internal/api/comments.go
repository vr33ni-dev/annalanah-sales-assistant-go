package api

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/domain"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/store"
)

// CommentResponse is what the API returns
type CommentResponse struct {
	ID         int                    `json:"id"`
	EntityType string                 `json:"entity_type"`
	EntityID   int                    `json:"entity_id"`
	ClientID   *int                   `json:"client_id,omitempty"`
	Author     *string                `json:"author,omitempty"`
	Body       string                 `json:"body"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt  string                 `json:"created_at"`
	UpdatedAt  string                 `json:"updated_at"`
}

func commentToResponse(c domain.Comment) CommentResponse {
	createdAt := ""
	if !c.CreatedAt.IsZero() {
		createdAt = c.CreatedAt.Format(time.RFC3339)
	}
	updatedAt := ""
	if !c.UpdatedAt.IsZero() {
		updatedAt = c.UpdatedAt.Format(time.RFC3339)
	}
	return CommentResponse{
		ID:         c.ID,
		EntityType: c.EntityType,
		EntityID:   c.EntityID,
		ClientID:   c.ClientID,
		Author:     c.Author,
		Body:       c.Body,
		Metadata:   c.Metadata,
		CreatedAt:  createdAt,
		UpdatedAt:  updatedAt,
	}
}

// ListComments GET /api/comments?entity_type=client&entity_id=123
// Also supports ?client_id=123 to return all comments for a client across all entity types.
func (h *Handler) ListComments(w http.ResponseWriter, r *http.Request) {
	qt := r.URL.Query()

	if clientIDStr := qt.Get("client_id"); clientIDStr != "" {
		cid, err := strconv.Atoi(clientIDStr)
		if err != nil {
			http.Error(w, "invalid client_id", http.StatusBadRequest)
			return
		}
		h.listCommentsByClientID(w, cid)
		return
	}

	entityType := qt.Get("entity_type")
	if entityType == "salesprocess" {
		entityType = "sales_process"
	}
	idStr := qt.Get("entity_id")
	if entityType == "" || idStr == "" {
		http.Error(w, "entity_type and entity_id are required (or use client_id)", http.StatusBadRequest)
		return
	}
	eid, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid entity_id", http.StatusBadRequest)
		return
	}

	comments, err := h.store.ListCommentsByEntity(entityType, eid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	out := make([]CommentResponse, 0, len(comments))
	for _, c := range comments {
		out = append(out, commentToResponse(c))
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (h *Handler) listCommentsByClientID(w http.ResponseWriter, clientID int) {
	comments, err := h.store.ListCommentsByClientID(clientID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	out := make([]CommentResponse, 0, len(comments))
	for _, c := range comments {
		out = append(out, commentToResponse(c))
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// CreateComment POST /api/comments
func (h *Handler) CreateComment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		EntityType string                 `json:"entity_type"`
		EntityID   int                    `json:"entity_id"`
		Author     *string                `json:"author,omitempty"`
		Body       string                 `json:"body"`
		Content    *string                `json:"content,omitempty"`
		Metadata   map[string]interface{} `json:"metadata,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	req.Body = strings.TrimSpace(req.Body)
	if req.Body == "" && req.Content != nil {
		req.Body = strings.TrimSpace(*req.Content)
	}
	if req.EntityType == "salesprocess" {
		req.EntityType = "sales_process"
	}

	if sess, ok := h.parseSession(r); ok {
		req.Author = &sess.Name
	} else if req.Author == nil {
		def := os.Getenv("DEFAULT_COMMENT_AUTHOR")
		if def == "" {
			def = "local-dev"
		}
		req.Author = &def
	}

	if req.EntityType == "" {
		http.Error(w, "entity_type is required", http.StatusBadRequest)
		return
	}
	if req.EntityID == 0 {
		http.Error(w, "entity_id is required and must be non-zero", http.StatusBadRequest)
		return
	}
	if req.Body == "" {
		http.Error(w, "body is required", http.StatusBadRequest)
		return
	}

	c, err := h.store.CreateComment(req.EntityType, req.EntityID, req.Author, req.Body, req.Metadata)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(commentToResponse(c))
}

// DeleteComment DELETE /api/comments/{id}
func (h *Handler) DeleteComment(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDFromURL(r.URL.Path)
	if !ok {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	if err := h.store.DeleteComment(id); err != nil {
		if err == store.ErrNotFound {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// UpdateComment PATCH /api/comments/{id}
func (h *Handler) UpdateComment(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDFromURL(r.URL.Path)
	if !ok {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var req struct {
		Author   *string                 `json:"author,omitempty"`
		Body     *string                 `json:"body,omitempty"`
		Content  *string                 `json:"content,omitempty"`
		Metadata *map[string]interface{} `json:"metadata,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.Body == nil && req.Content != nil {
		req.Body = req.Content
	}
	if req.Body != nil {
		s := strings.TrimSpace(*req.Body)
		req.Body = &s
	}

	if req.Author == nil && req.Body == nil && req.Metadata == nil {
		http.Error(w, "no fields to update", http.StatusBadRequest)
		return
	}

	c, err := h.store.UpdateComment(id, req.Author, req.Body, req.Metadata)
	if err != nil {
		if err == store.ErrNotFound {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(commentToResponse(c))
}

// insertCommentsForEntity delegates to the store for bulk comment insertion.
// clientID should be the owning client; for entity_type="client" it equals entityID.
func (h *Handler) insertCommentsForEntity(entityType string, entityID int, clientID int, comments []domain.Comment) error {
	return h.store.InsertCommentsForEntity(entityType, entityID, clientID, comments)
}
