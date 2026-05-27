package domain

type Contract struct {
	ID             int       `json:"id"`
	ClientID       int       `json:"client_id"`
	SalesProcessID *int      `json:"sales_process_id"`
	StartDate      string    `json:"start_date"`
	CreatedAt      *string   `json:"created_at,omitempty"`
	EndDate        *string   `json:"end_date,omitempty"`
	DurationMonths int       `json:"duration_months"`
	RevenueTotal   float64   `json:"revenue_total"`
	PaymentFreq    string    `json:"payment_frequency"`
	Comments       []Comment `json:"comments,omitempty"`
}
