package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/vr33ni-dev/sales-assistant/api"
	"github.com/vr33ni-dev/sales-assistant/db"
)

func main() {
	cfg, err := api.LoadConfig()
	if err != nil {
		log.Fatal(err)
	}

	// connect DB (pass the DSN so db.Connect doesn’t need to read env)
	database := db.ConnectDSN(cfg.DatabaseURL)
	defer database.Close()

	// router
	r := api.NewRouterWithConfig(database, cfg)

	fmt.Printf("🚀 %s server listening on :%s\n", cfg.AppEnv, cfg.Port)
	log.Fatal(http.ListenAndServe(":"+cfg.Port, r))
}
