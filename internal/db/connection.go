package db

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/lib/pq"
)

type Store struct{ db *sql.DB }

var (
	openDB  = sql.Open
	fatalFn = log.Fatal
	pingFn  = func(db *sql.DB) error { return db.Ping() }
)

func New(dsn string) (*Store, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) DB() *sql.DB { return s.db }

func ConnectDSN(dsn string) *sql.DB {
	if dsn == "" {
		fatalFn("DATABASE_URL not set")
	}
	db, err := openDB("postgres", dsn)
	if err != nil {
		fatalFn("Cannot open DB:", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)
	if err := pingFn(db); err != nil {
		fatalFn("Cannot reach DB:", err)
	}
	return db
}
