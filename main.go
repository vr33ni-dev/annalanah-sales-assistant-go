package main

import (
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"

	"github.com/vr33ni-dev/annalanah-sales-assistant-go/api"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/db"
)

func main() {

	cfg, err := api.LoadConfig()
	if err != nil {
		log.Fatal(err)
	}

	// connect DB (pass the DSN so db.Connect doesn’t need to read env)
	database := db.ConnectDSN(cfg.DatabaseURL)
	log.Printf("DB: %q", cfg.DatabaseURL)

	// Optionally start pprof listener if PPROF_ADDR is set
	if pprofAddr := os.Getenv("PPROF_ADDR"); pprofAddr != "" {
		go func() {
			log.Printf("pprof listening on %s", pprofAddr)
			if err := http.ListenAndServe(pprofAddr, nil); err != nil && err != http.ErrServerClosed {
				log.Printf("pprof failed: %v", err)
			}
		}()
	}

	// router
	r := api.NewRouterWithConfig(database, cfg)

	// Print friendly startup message
	log.Printf("version=%s", cfg.AppEnv)
	fmt.Printf("🚀 %s server listening on :%s\n", cfg.AppEnv, cfg.Port)

	log.Fatal(http.ListenAndServe(":"+cfg.Port, r))
}
