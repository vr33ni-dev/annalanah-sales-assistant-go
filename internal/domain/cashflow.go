package domain

type CashflowEntry struct {
	ID         int     `json:"id"`
	ContractID int     `json:"contract_id"`
	DueDate    *string `json:"due_date"`
	Amount     float64 `json:"amount"`
	Status     string  `json:"status"`
	UpdatedAt  *string `json:"updated_at,omitempty"`
}

type CashflowForecastRow struct {
	Month     string  `json:"month"`
	Confirmed float64 `json:"confirmed"`
	Potential float64 `json:"potential"`
}

type MonthConfirmed struct {
	Month     string  `json:"month"`
	Confirmed float64 `json:"confirmed"`
}
