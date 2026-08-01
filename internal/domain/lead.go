package domain

type Lead struct {
	ID                int     `json:"id"`
	Name              string  `json:"name"`
	Email             string  `json:"email"`
	Phone             string  `json:"phone"`
	Source            string  `json:"source"`
	SourceStageID     *int    `json:"source_stage_id,omitempty"`
	SourceStageName   *string `json:"source_stage_name,omitempty"`
	Converted         bool    `json:"converted"`
	ConvertedClientID *int    `json:"converted_client_id,omitempty"`
	CreatedAt         *string `json:"created_at,omitempty"`
}
