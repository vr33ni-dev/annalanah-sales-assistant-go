package factory

import (
	"testing"

	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/api/testhelpers"
)

type APITestSuite struct {
	T  *testing.T
	DB *testhelpers.TestDB
}

func NewSuite(t *testing.T) *APITestSuite {
	t.Helper()

	db, err := testhelpers.SetupPostgres(t)
	if err != nil {
		t.Skipf("skipping integration tests (docker unavailable): %v", err)
	}

	return &APITestSuite{
		T:  t,
		DB: db,
	}
}

// NewSuiteFromTestDB creates a suite that reuses an existing TestDB (shared across tests).
func NewSuiteFromTestDB(t *testing.T, db *testhelpers.TestDB) *APITestSuite {
	t.Helper()
	if db == nil {
		// fallback to per-test DB when no shared DB is available
		return NewSuite(t)
	}

	return &APITestSuite{T: t, DB: db}
}

// Cleanup tears down resources for non-shared suites. For shared suites managed by
// integration TestMain, avoid tearing down the shared DB here.
func (s *APITestSuite) Cleanup() {
	if s == nil || s.DB == nil {
		return
	}
	s.DB.TearDown(s.T)
}
