package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/joho/godotenv"

	"github.com/vr33ni-dev/sales-assistant/api"
	"github.com/vr33ni-dev/sales-assistant/db"
)

func main() {
	// load .env (no-op if missing)
	_ = godotenv.Load()

	// connect to db
	database := db.Connect()
	defer database.Close()

	// Build the router (we'll add auth mounting INSIDE api.NewRouter)
	r := api.NewRouter(database)

	// start server
	port := os.Getenv("PORT") // << use PORT, not DB_PORT
	if port == "" {
		port = "8080"
	}
	fmt.Printf("🚀 Server running on http://localhost:%s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}
