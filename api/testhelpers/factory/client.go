package factory

type Client struct {
	ID int
}

func (s *APITestSuite) CreateClient() Client {
	var id int
	err := s.DB.DB.QueryRow(`
		INSERT INTO clients (name, status)
		VALUES ('Client ' || NOW()::text, 'inactive')
		RETURNING id
	`).Scan(&id)

	if err != nil {
		s.T.Fatalf("CreateClient failed: %v", err)
	}

	return Client{ID: id}
}
