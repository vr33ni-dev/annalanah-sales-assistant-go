package db

import (
	"database/sql"
	"log"
	"os"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

func Connect() *sql.DB {
	// Load .env (for dev)
	_ = godotenv.Load()

	dsn := os.Getenv("DB_URL")
	if dsn == "" {
		log.Fatal("DB_URL not set in .env")
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
