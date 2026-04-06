package factory

type Contract struct {
	ID             int
	ClientID       int
	SalesProcessID int
	StartDate      string
	DurationMonths int
	RevenueTotal   float64
	PaymentFreq    string
}

type ContractOption func(*Contract)

func WithDuration(months int) ContractOption {
	return func(c *Contract) {
		c.DurationMonths = months
	}
}

func WithRevenue(amount float64) ContractOption {
	return func(c *Contract) {
		c.RevenueTotal = amount
	}
}

func WithStartDate(date string) ContractOption {
	return func(c *Contract) {
		c.StartDate = date
	}
}

func (s *APITestSuite) CreateContract(
	clientID, processID int,
	opts ...ContractOption,
) Contract {
	c := Contract{
		StartDate:      "2025-01-01",
		DurationMonths: 6,
		RevenueTotal:   5000,
		PaymentFreq:    "monthly",
	}

	for _, opt := range opts {
		opt(&c)
	}

	var id int
	err := s.DB.DB.QueryRow(`
		INSERT INTO contracts (
			client_id,
			sales_process_id,
			start_date,
			duration_months,
			revenue_total,
			payment_frequency
		)
		VALUES ($1, $2, $3::date, $4, $5, $6)
		RETURNING id
	`,
		clientID,
		processID,
		c.StartDate,
		c.DurationMonths,
		c.RevenueTotal,
		c.PaymentFreq,
	).Scan(&id)

	if err != nil {
		s.T.Fatalf("CreateContract failed: %v", err)
	}

	c.ID = id
	c.ClientID = clientID
	c.SalesProcessID = processID

	return c
}
