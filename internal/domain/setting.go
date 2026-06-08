package domain

type AppSetting struct {
	Key          string   `json:"key"`
	ValueNumeric *float64 `json:"value_numeric,omitempty"`
	ValueText    *string  `json:"value_text,omitempty"`
	UpdatedAt    *string  `json:"updated_at,omitempty"`
}
