package api

import (
	"database/sql"
	"encoding/json"
	"time"
)

func (h *Handler) loadSalesProcessResponse(id int) (SalesProcessResponse, error) {
	row := h.DB.QueryRow(`
	  SELECT
	    sp.id,
	    sp.client_id,
	    c.name  AS client_name,
	    c.email AS client_email,
	    c.phone AS client_phone,
	    c.source AS client_source,
	    c.completed_at AS completed_at,
	    sp.stage,
			sp.initial_contact_date,
	    sp.follow_up_date,
	    sp.follow_up_result,
	    sp.closed,
	    CASE WHEN COALESCE(sp.closed, false) THEN sp.revenue ELSE NULL END AS revenue,
	    sp.stage_id
	  FROM sales_process sp
	  JOIN clients c ON c.id = sp.client_id
	  WHERE sp.id = $1
	`, id)

	var updated SalesProcessResponse
	var completedAt sql.NullTime
	if err := row.Scan(
		&updated.ID,
		&updated.ClientID,
		&updated.ClientName,
		&updated.ClientEmail,
		&updated.ClientPhone,
		&updated.ClientSource,
		&completedAt,
		&updated.Stage,
		&updated.InitialContactDate,
		&updated.FollowUpDate,
		&updated.FollowUpResult,
		&updated.Closed,
		&updated.Revenue,
		&updated.StageID,
	); err != nil {
		return SalesProcessResponse{}, err
	}
	updated.CompletedAt = nullTimeToString(completedAt, "2006-01-02")

	commentRows, err := h.DB.Query(`
		SELECT id, client_id, entity_type, entity_id, author, body, metadata, created_at, updated_at
		FROM comments
		WHERE client_id = $1
		ORDER BY created_at DESC
	`, updated.ClientID)
	if err == nil {
		defer commentRows.Close()

		var comments []CommentResponse
		for commentRows.Next() {
			var id int
			var cid sql.NullInt64
			var entityType string
			var entityID int
			var author sql.NullString
			var body string
			var metadata sql.NullString
			var created, updatedAt time.Time
			if err := commentRows.Scan(&id, &cid, &entityType, &entityID, &author, &body, &metadata, &created, &updatedAt); err == nil {
				var meta map[string]interface{}
				if metadata.Valid && metadata.String != "" {
					_ = json.Unmarshal([]byte(metadata.String), &meta)
				}
				a := nullStringToPtr(author)
				var cidPtr *int
				if cid.Valid {
					v := int(cid.Int64)
					cidPtr = &v
				}
				comments = append(comments, CommentResponse{
					ID: id, ClientID: cidPtr, EntityType: entityType, EntityID: entityID,
					Author: a, Body: body, Metadata: meta,
					CreatedAt: created.Format(time.RFC3339), UpdatedAt: updatedAt.Format(time.RFC3339),
				})
			}
		}
		if comments == nil {
			comments = []CommentResponse{}
		}
		updated.Comments = comments
	}

	return updated, nil
}
