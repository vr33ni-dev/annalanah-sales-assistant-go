package db

import (
	"database/sql"
	"log"
	"time"

	_ "github.com/lib/pq"
)

var (
	openDB  = sql.Open
	fatalFn = log.Fatal
	pingFn  = func(db *sql.DB) error { return db.Ping() }
)

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
