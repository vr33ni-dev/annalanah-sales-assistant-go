package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// CommentCreateRequest represents a client-supplied comment payload
type CommentCreateRequest struct {
	Author   *string                `json:"author,omitempty"`
	Body     string                 `json:"body"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// CommentResponse is what the API returns
type CommentResponse struct {
	ID         int                    `json:"id"`
	EntityType string                 `json:"entity_type"`
	EntityID   int                    `json:"entity_id"`
	Author     *string                `json:"author,omitempty"`
	Body       string                 `json:"body"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt  string                 `json:"created_at"`
	UpdatedAt  string                 `json:"updated_at"`
}

// ListComments GET /api/comments?entity_type=client&entity_id=123
func (h *Handler) ListComments(w http.ResponseWriter, r *http.Request) {
	qt := r.URL.Query()
	entityType := qt.Get("entity_type")
	idStr := qt.Get("entity_id")
	if entityType == "" || idStr == "" {
		http.Error(w, "entity_type and entity_id are required", http.StatusBadRequest)
		return
	}
	eid, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid entity_id", http.StatusBadRequest)
		return
	}

	rows, err := h.DB.Query(`
		SELECT id, author, body, metadata, created_at, updated_at
		FROM comments
		WHERE entity_type = $1 AND entity_id = $2
		ORDER BY created_at DESC
	`, entityType, eid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var out []CommentResponse
	for rows.Next() {
		var id int
		var author sql.NullString
		var body string
		var metadata sql.NullString
		var created, updated sql.NullTime

		if err := rows.Scan(&id, &author, &body, &metadata, &created, &updated); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var meta map[string]interface{}
		if metadata.Valid && metadata.String != "" {
			_ = json.Unmarshal([]byte(metadata.String), &meta)
		}

		var a *string
		if author.Valid {
			s := author.String
			a = &s
		}

		createdAt := ""
		if created.Valid {
			createdAt = created.Time.Format(time.RFC3339)
		}
		updatedAt := ""
		if updated.Valid {
			updatedAt = updated.Time.Format(time.RFC3339)
		}

		out = append(out, CommentResponse{
			ID:         id,
			EntityType: entityType,
			EntityID:   eid,
			Author:     a,
			Body:       body,
			Metadata:   meta,
			CreatedAt:  createdAt,
			UpdatedAt:  updatedAt,
		})
	}

	if err := rows.Err(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if out == nil {
		out = []CommentResponse{}
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
	// log the decoded request to help diagnose missing/incorrect fields
	if b, err := json.Marshal(req); err == nil {
		log.Printf("CreateComment: decoded request=%s", string(b))
	}
	// normalize body by trimming whitespace
	req.Body = strings.TrimSpace(req.Body)
	// accept frontend that sends `content` instead of `body`
	if req.Body == "" && req.Content != nil {
		req.Body = strings.TrimSpace(*req.Content)
	}
	// If the requester is authenticated, prefer server-side session name
	// as the comment author. If no session exists (local/dev) and no author
	// was supplied, use a configurable dummy author so comments still show
	// a sensible author when only the backend is running.
	if sess, ok := h.parseSession(r); ok {
		// overwrite any provided author with the logged-in user's name
		req.Author = &sess.Name
	} else {
		if req.Author == nil {
			def := os.Getenv("DEFAULT_COMMENT_AUTHOR")
			if def == "" {
				def = "local-dev"
			}
			req.Author = &def
		}
	}

	// Debugging: log which author will be stored (helps diagnose missing session cookie)
	if req.Author != nil {
		log.Printf("CreateComment: chosen author=%q, entity=%s/%d", *req.Author, req.EntityType, req.EntityID)
	} else {
		log.Printf("CreateComment: no author chosen, entity=%s/%d", req.EntityType, req.EntityID)
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

	// metadata -> jsonb
	metaBytes, _ := json.Marshal(req.Metadata)

	var id int
	var created, updated time.Time
	err := h.DB.QueryRow(
		`INSERT INTO comments (entity_type, entity_id, author, body, metadata) VALUES ($1,$2,$3,$4,$5::jsonb) RETURNING id, created_at, updated_at`,
		req.EntityType, req.EntityID, req.Author, req.Body, string(metaBytes),
	).Scan(&id, &created, &updated)
	if err != nil {
		log.Printf("CreateComment: insert failed entity=%s id=%d author=%v body=%q metadata=%s err=%v",
			req.EntityType, req.EntityID, req.Author, req.Body, string(metaBytes), err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := CommentResponse{
		ID:         id,
		EntityType: req.EntityType,
		EntityID:   req.EntityID,
		Author:     req.Author,
		Body:       req.Body,
		Metadata:   req.Metadata,
		CreatedAt:  created.Format(time.RFC3339),
		UpdatedAt:  updated.Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}

// DeleteComment DELETE /api/comments/{id}
func (h *Handler) DeleteComment(w http.ResponseWriter, r *http.Request) {
	// naive path parse: last segment is id
	id, ok := parseIDFromURL(r.URL.Path)
	if !ok {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	res, err := h.DB.Exec(`DELETE FROM comments WHERE id = $1`, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	nr, _ := res.RowsAffected()
	if nr == 0 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// UpdateComment PATCH /api/comments/{id}
// Accepts partial updates for author, body and metadata. Only provided fields are updated.
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

	// normalize body content when frontend uses `content` key
	if req.Body == nil && req.Content != nil {
		req.Body = req.Content
	}
	if req.Body != nil {
		s := strings.TrimSpace(*req.Body)
		req.Body = &s
	}

	// Build dynamic SET clause only for provided fields
	var sets []string
	var args []interface{}
	idx := 1
	if req.Author != nil {
		sets = append(sets, fmt.Sprintf("author = $%d", idx))
		args = append(args, *req.Author)
		idx++
	}
	if req.Body != nil {
		sets = append(sets, fmt.Sprintf("body = $%d", idx))
		args = append(args, *req.Body)
		idx++
	}
	if req.Metadata != nil {
		metaBytes, _ := json.Marshal(req.Metadata)
		sets = append(sets, fmt.Sprintf("metadata = $%d::jsonb", idx))
		args = append(args, string(metaBytes))
		idx++
	}

	if len(sets) == 0 {
		http.Error(w, "no fields to update", http.StatusBadRequest)
		return
	}

	// always update updated_at
	sets = append(sets, fmt.Sprintf("updated_at = now()"))

	// final arg is id
	args = append(args, id)

	query := fmt.Sprintf(`UPDATE comments SET %s WHERE id = $%d RETURNING id, entity_type, entity_id, author, body, metadata, created_at, updated_at`, strings.Join(sets, ", "), idx)

	var (
		cid        int
		entityType string
		entityID   int
		author     sql.NullString
		body       string
		metadata   sql.NullString
		created    sql.NullTime
		updated    sql.NullTime
	)

	row := h.DB.QueryRow(query, args...)
	if err := row.Scan(&cid, &entityType, &entityID, &author, &body, &metadata, &created, &updated); err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var meta map[string]interface{}
	if metadata.Valid && metadata.String != "" {
		_ = json.Unmarshal([]byte(metadata.String), &meta)
	}

	var a *string
	if author.Valid {
		s := author.String
		a = &s
	}

	createdAt := ""
	if created.Valid {
		createdAt = created.Time.Format(time.RFC3339)
	}
	updatedAt := ""
	if updated.Valid {
		updatedAt = updated.Time.Format(time.RFC3339)
	}

	resp := CommentResponse{
		ID:         cid,
		EntityType: entityType,
		EntityID:   entityID,
		Author:     a,
		Body:       body,
		Metadata:   meta,
		CreatedAt:  createdAt,
		UpdatedAt:  updatedAt,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// insertCommentsForEntity inserts a list of comment payloads for the given entity.
func (h *Handler) insertCommentsForEntity(entityType string, entityID int, comments []CommentCreateRequest) error {
	for _, c := range comments {
		metaBytes, _ := json.Marshal(c.Metadata)
		_, err := h.DB.Exec(`INSERT INTO comments (entity_type, entity_id, author, body, metadata) VALUES ($1,$2,$3,$4,$5::jsonb)`,
			entityType, entityID, c.Author, c.Body, string(metaBytes))
		if err != nil {
			return err
		}
	}
	return nil
}
