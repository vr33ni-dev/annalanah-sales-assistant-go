package domain

import "time"

// Comment represents a client-supplied comment payload
type Comment struct {
	ID         int                    `json:"id"`
	EntityType string                 `json:"entity_type"`
	EntityID   int                    `json:"entity_id"`
	ClientID   *int                   `json:"client_id"`
	Author     *string                `json:"author,omitempty"`
	Body       string                 `json:"body"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt  time.Time              `json:"created_at"`
	UpdatedAt  time.Time              `json:"updated_at"`
}
