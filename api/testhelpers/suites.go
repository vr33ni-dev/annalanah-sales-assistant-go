package testhelpers

import (
	"testing"

	"github.com/vr33ni-dev/annalanah-sales-assistant-go/api"
)

/* Provides DB + handler + helpers */
type APITestSuite struct {
	T       *testing.T
	DB      *TestDB
	Handler *api.Handler
}

func NewAPITestSuite(t *testing.T) *APITestSuite {
	db := SetupPostgres(t)

	return &APITestSuite{
		T:       t,
		DB:      db,
		Handler: &api.Handler{DB: db.DB},
	}
}

func (s *APITestSuite) TearDown() {
	s.DB.TearDown(s.T)
}

/*
	SeedBasicData():

- seeds fixed rows
- good for bootstrapping simple data
- not flexible
- does not scale well when tests vary
*/
func (s *APITestSuite) SeedBasicData(t *testing.T) {
	DB := s.DB.DB

	_, err := DB.Exec(`INSERT INTO clients (id, name) VALUES (1, 'Client A')`)
	if err != nil {
		t.Fatalf("seed client: %v", err)
	}

	_, err = DB.Exec(`
		INSERT INTO sales_process (id, client_id, stage)
VALUES (2, 1, 'follow_up');

	`)
	if err != nil {
		t.Fatalf("seed sales_process: %v", err)
	}
}

func (s *APITestSuite) Cleanup(t *testing.T) {
	s.ResetDB(t)
}

func (s *APITestSuite) ResetDB(t *testing.T) {
	t.Helper()

	// drop only user-defined tables
	_, err := s.DB.DB.Exec(`
        DO $$
        DECLARE
            r RECORD;
        BEGIN
            FOR r IN (
                SELECT tablename 
                FROM pg_tables 
                WHERE schemaname='public'
                  AND tablename NOT LIKE 'pg_%'
                  AND tablename NOT LIKE 'sql_%'
            )
            LOOP
                EXECUTE 'DROP TABLE IF EXISTS "' || r.tablename || '" CASCADE';
            END LOOP;
        END $$;
    `)

	if err != nil {
		t.Fatalf("failed to drop tables: %v", err)
	}

	// reload migrations
	loadMigrations(s.DB.DB, t)
}
