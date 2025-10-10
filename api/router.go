package api

import (
	"database/sql"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
)

func NewRouter(db *sql.DB) *chi.Mux {
	h := &Handler{DB: db}
	r := chi.NewRouter()

	// === Auth init (panic on misconfig during boot is fine) ===
	if err := h.InitAuth(); err != nil {
		panic(err)
	}

	// === CORS (dev: allow Vite on 5002). Keep credentials true for cookies. ===
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:5002"}, // adjust/add prod origin(s) later
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// === Public endpoints ===
	// health
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })

	// auth endpoints (+ /api/me)
	h.MountAuthRoutes(r)

	// === Protected API ===
	r.Route("/api", func(pr chi.Router) {
		pr.Use(h.RequireAuth)

		// Clients
		pr.Get("/clients", h.ListClients)
		pr.Post("/clients", h.CreateClient)

		// Sales processes
		pr.Get("/sales", h.ListSalesProcesses)
		pr.Post("/sales", h.CreateSalesProcess)
		pr.Patch("/sales/{id}", h.UpdateSalesProcess)
		pr.Post("/sales/start", h.StartSalesProcess)

		// Contracts
		pr.Get("/contracts", h.ListContracts)
		pr.Post("/contracts", h.CreateContract)
		pr.Patch("/contracts/{id}", h.UpdateContract)

		// Stages (Bühnen)
		pr.Get("/stages", h.ListStages)
		pr.Post("/stages", h.CreateStage)
		pr.Patch("/stages/{id}/stats", h.UpdateStageStats)

		// Stage participants
		pr.Post("/stages/{id}/participants", h.AddStageParticipant)
		pr.Patch("/stages/{id}/participants/{participant_id}", h.UpdateStageParticipant)

		// Assign client to stage
		pr.Post("/stages/{id}/assign-client", h.AssignClientToStage)

		// Cashflow
		pr.Get("/cashflow/forecast", h.CashflowForecast)

		// App settings
		pr.Get("/settings", h.ListSettings)
		pr.Get("/settings/{key}", h.GetSetting)
		pr.Put("/settings/{key}", h.UpsertSetting)
	})

	return r
}
