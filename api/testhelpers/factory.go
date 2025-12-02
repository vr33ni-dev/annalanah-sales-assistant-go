package testhelpers

/* Factory:
- dynamically creates correct data
- always consistent with schema
- auto-handles foreign keys
- avoids hardcoding IDs
- makes tests shorter and more expressive
- scales to dozens of test suites effortlessly */

type Client struct {
	ID int
}

func (s *APITestSuite) CreateClient() Client {
	var id int
	err := s.DB.DB.QueryRow(`
        INSERT INTO clients (name)
        VALUES ('Client ' || NOW()::text)
        RETURNING id
    `).Scan(&id)

	if err != nil {
		s.T.Fatalf("CreateClient failed: %v", err)
	}

	return Client{ID: id}
}

type SalesProcess struct {
	ID       int
	ClientID int
}

func (s *APITestSuite) CreateSalesProcessForClient(clientID int) SalesProcess {
	var id int
	err := s.DB.DB.QueryRow(`
        INSERT INTO sales_process (client_id, stage)
        VALUES ($1, 'follow_up')
        RETURNING id
    `, clientID).Scan(&id)

	if err != nil {
		s.T.Fatalf("CreateSalesProcessForClient failed: %v", err)
	}

	return SalesProcess{ID: id, ClientID: clientID}
}

type Contract struct {
	ID             int
	ClientID       int
	SalesProcessID int
	StartDate      string
	DurationMonths int
	RevenueTotal   float64
	PaymentFreq    string
}

func (s *APITestSuite) CreateContract(clientID, processID int) Contract {
	var id int

	// Choose sensible defaults
	start := "2025-01-01"
	duration := 6
	revenue := 5000.0
	payFreq := "monthly"

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
		start,
		duration,
		revenue,
		payFreq,
	).Scan(&id)

	if err != nil {
		s.T.Fatalf("CreateContract failed: %v", err)
	}

	return Contract{
		ID:             id,
		ClientID:       clientID,
		SalesProcessID: processID,
		StartDate:      start,
		DurationMonths: duration,
		RevenueTotal:   revenue,
		PaymentFreq:    payFreq,
	}
}
