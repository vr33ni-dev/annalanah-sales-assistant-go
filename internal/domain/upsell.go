package domain

import "time"

// Upsells
// Using Pointers because PostgreSQL can return NULL, and Go's plain string cannot represent NULL, only "" and these fields can return nil
type ContractUpsell struct {
	ID                     int        `json:"id"`
	SalesProcessID         int        `json:"sales_process_id"`
	ClientID               int        `json:"client_id"`
	UpsellDate             *string    `json:"upsell_date"`
	UpsellResult           *string    `json:"upsell_result"` // "verlaengerung" or "keine_verlaengerung"`
	UpsellRevenue          *float64   `json:"upsell_revenue,omitempty"`
	ContractStartDate      *time.Time `json:"contract_start_date"`
	ContractDurationMonths *int       `json:"contract_duration_months"`
	ContractFrequency      *string    `json:"contract_frequency"`
	PreviousContractID     *int       `json:"previous_contract_id,omitempty"`
	NewContractID          *int       `json:"new_contract_id,omitempty"`
	CreatedAt              *string    `json:"created_at"`
	UpdatedAt              *string    `json:"updated_at"`
}
