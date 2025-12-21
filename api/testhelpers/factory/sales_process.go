package factory

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

	return SalesProcess{
		ID:       id,
		ClientID: clientID,
	}
}
