package factory

import (
	"testing"

	"github.com/vr33ni-dev/annalanah-sales-assistant-go/api/testhelpers"
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

func (s *APITestSuite) Cleanup() {
	s.DB.TearDown(s.T)
}
