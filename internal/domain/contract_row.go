package domain

// ContractRow holds the flat result of a contract query (with joined client name and computed fields).
// Revenue fields are stored as Brutto (raw DB values); the handler converts to Netto.
type ContractRow struct {
	ID                int
	ClientID          int
	ClientName        string
	SalesProcessID    *int
	StartDate         string
	EndDate           *string
	CreatedAt         *string
	UpdatedAt         *string
	DurationMonths    int
	RevenueBrutto     float64
	PaymentFreq       string
	BaseMonthlyBrutto float64
	NextDueDate       *string
	Source            string
	Comments          []Comment
	CashflowEntries   []CashflowEntry
	Chain             []ContractRow // populated only by GetContractByID
	EndDateOverride   *string       // optional override for the end date, used for paused contracts
}

// ContractNotifyData holds the data needed to send a new-contract notification email.
type ContractNotifyData struct {
	ClientName  string
	ClosureDate string
	Source      string
	StageName   string
	NextDueDate string
}
