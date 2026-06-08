package db

import (
	"database/sql"
	"errors"
	"testing"
)

func TestConnectDSN(t *testing.T) {
	t.Run("empty DSN triggers fatal", func(t *testing.T) {
		called := false
		fatalFn = func(v ...interface{}) {
			called = true
			panic("fatal called")
		}
		defer func() { _ = recover() }()
		ConnectDSN("")
		if !called {
			t.Errorf("expected fatalFn to be called")
		}
	})

	t.Run("openDB returns error triggers fatal", func(t *testing.T) {
		fatalCalled := false
		fatalFn = func(v ...interface{}) {
			fatalCalled = true
			panic("fatal called")
		}
		origOpen := openDB
		openDB = func(driver, dsn string) (*sql.DB, error) {
			return nil, errors.New("open failed")
		}
		defer func() {
			openDB = origOpen
			_ = recover()
		}()
		ConnectDSN("bad_dsn")
		if !fatalCalled {
			t.Errorf("expected fatalFn to be called")
		}
	})

	t.Run("ping returns error triggers fatal", func(t *testing.T) {
		fatalCalled := false
		fatalFn = func(v ...interface{}) {
			fatalCalled = true
			panic("fatal called")
		}
		origOpen := openDB
		openDB = func(driver, dsn string) (*sql.DB, error) {
			return &sql.DB{}, nil
		}
		defer func() {
			openDB = origOpen
			_ = recover()
		}()

		// stub ping to return error
		origPing := pingFn
		pingFn = func(db *sql.DB) error { return errors.New("ping failed") }
		defer func() { pingFn = origPing }()

		ConnectDSN("postgres://invalid@localhost")
		if !fatalCalled {
			t.Errorf("expected fatalFn to be called")
		}
	})

	t.Run("valid DSN returns *sql.DB", func(t *testing.T) {
		fatalFn = func(v ...interface{}) { t.Fatalf("fatal called unexpectedly: %v", v) }

		origOpen := openDB
		openDB = func(driver, dsn string) (*sql.DB, error) {
			return &sql.DB{}, nil
		}
		defer func() { openDB = origOpen }()

		// stub ping to succeed
		origPing := pingFn
		pingFn = func(db *sql.DB) error { return nil }
		defer func() { pingFn = origPing }()

		got := ConnectDSN("postgres://user:pass@localhost/db")
		if got == nil {
			t.Fatalf("expected *sql.DB, got nil")
		}
	})
}
