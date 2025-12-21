package factory

import "time"

type CashflowEntry struct {
	ID         int
	ContractID int
	DueDate    time.Time
	Amount     float64
}

// ------------------------------------------------------------------
// Preferred semantic helpers
// ------------------------------------------------------------------

func (s *APITestSuite) CreatePaidCashflow(
	contractID int,
	due time.Time,
	amount float64,
) {
	_, err := s.DB.DB.Exec(`
		INSERT INTO cashflow_entries (contract_id, due_date, amount, status)
		VALUES ($1, $2, $3, 'paid')
	`, contractID, due, amount)

	if err != nil {
		s.T.Fatalf("CreatePaidCashflow failed: %v", err)
	}
}

func (s *APITestSuite) CreatePendingCashflow(
	contractID int,
	due time.Time,
	amount float64,
) {
	_, err := s.DB.DB.Exec(`
		INSERT INTO cashflow_entries (contract_id, due_date, amount, status)
		VALUES ($1, $2, $3, 'pending')
	`, contractID, due, amount)

	if err != nil {
		s.T.Fatalf("CreatePendingCashflow failed: %v", err)
	}
}

// ------------------------------------------------------------------
// Backward compatibility (temporary)
// ------------------------------------------------------------------

// CreateCashflowEntry exists for backward compatibility with older tests.
// It creates a PAID cashflow entry by default.
//
// TODO: migrate all tests to CreatePaidCashflow / CreatePendingCashflow
// and remove this method.
func (s *APITestSuite) CreateCashflowEntry(
	contractID int,
	due time.Time,
	amount float64,
) {
	s.CreatePaidCashflow(contractID, due, amount)
}
