package domain

import "time"

type Client struct {
	ID            int        `json:"id"`
	Name          string     `json:"name"`
	Email         string     `json:"email"`
	Phone         string     `json:"phone"`
	Source        string     `json:"source"`
	SourceStageID *int       `json:"source_stage_id,omitempty"`
	Status        string     `json:"status"` // "active", "lost", "initial_call_scheduled", "follow_up_scheduled", "awaiting_response", "inactive"
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
	Comments      []Comment  `json:"comments,omitempty"`
}
