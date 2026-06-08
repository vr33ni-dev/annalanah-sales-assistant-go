package domain

type ContractSummary struct {
	ContractID    int
	ClientID      int
	ClientName    string
	StartDate     string
	EndDate       *string
	RevenueBrutto float64
}

type DashboardKPIsRaw struct {
	RenewalRevenueBrutto      float64
	NewCustomerRevenueBrutto  float64
	VerlaengerungCount        int
	KeineVerlaengerungCount   int
	Verlaengerungsquote       *float64
	TotalRevenueBrutto        float64
	CLVActiveClientsBrutto    float64
	GesamtCLVBrutto           float64
	ActiveContractsCount      int
	ActiveRevenueBrutto       float64
	WonNewCount               int
	DecidedNewCount           int
}

type MonthlyKPIRaw struct {
	Month   int
	Revenue float64
	Won     int
	Decided int
}
