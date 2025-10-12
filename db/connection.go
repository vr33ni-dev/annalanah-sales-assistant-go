// db/connect.go
package db

import (
	"database/sql"
	"log"

	_ "github.com/lib/pq"
)

func ConnectDSN(dsn string) *sql.DB {
	if dsn == "" {
		log.Fatal("DATABASE_URL not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatal("Cannot open DB:", err)
	}
	if err := db.Ping(); err != nil {
		log.Fatal("Cannot reach DB:", err)
	}
	return db
}
