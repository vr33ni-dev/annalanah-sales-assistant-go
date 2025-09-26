package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/vr33ni-dev/sales-assistant/api"
	"github.com/vr33ni-dev/sales-assistant/db"

	"github.com/joho/godotenv"
)

func main() {
	// load .env
	_ = godotenv.Load()

	// connect to db
	database := db.Connect()
	defer database.Close()

	r := api.NewRouter(database)

	// start server
	port := os.Getenv("DB_PORT")
	if port == "" {
		port = "8080"
	}
	fmt.Printf("🚀 Server running on http://localhost:%s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}
