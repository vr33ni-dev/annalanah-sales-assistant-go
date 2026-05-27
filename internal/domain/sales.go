package domain

import "time"

type SalesProcess struct {
	ID                 int       `json:"id"`
	ClientID           int       `json:"client_id"`
	ClientName         string    `json:"client_name"`
	ClientEmail        *string   `json:"client_email,omitempty"`
	ClientPhone        *string   `json:"client_phone,omitempty"`
	ClientSource       *string   `json:"client_source,omitempty"`
	CompletedAt        *string   `json:"completed_at,omitempty"`
	Stage              string    `json:"stage"`
	CreatedAt          time.Time `json:"created_at"`
	InitialContactDate *string   `json:"initial_contact_date"`
	FollowUpDate       *string   `json:"follow_up_date"`
	FollowUpResult     *bool     `json:"follow_up_result"`
	Closed             *bool     `json:"closed"`
	Revenue            *float64  `json:"revenue"`
	StageID            *int      `json:"stage_id"`
	LeadID             *int      `json:"lead_id,omitempty"`
	Comments           []Comment `json:"comments,omitempty"`
}
