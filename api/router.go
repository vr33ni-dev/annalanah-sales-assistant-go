package api

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
)

func NewRouter(db *sql.DB) *chi.Mux {
	h := &Handler{DB: db}
	r := chi.NewRouter()

	// CORS middleware — allow Vite dev origin
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:5002"}, // change if your frontend runs on another origin
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Prefix everything with /api
	r.Route("/api", func(r chi.Router) {
		// Clients
		r.Get("/clients", h.ListClients)
		r.Post("/clients", h.CreateClient)

		// Sales processes
		r.Get("/sales", h.ListSalesProcesses)
		r.Post("/sales", h.CreateSalesProcess)
		r.Patch("/sales/{id}", h.UpdateSalesProcess)
		r.Post("/sales/start", h.StartSalesProcess)

		// Contracts
		r.Get("/contracts", h.ListContracts)
		r.Post("/contracts", h.CreateContract)
		r.Patch("/contracts/{id}", h.UpdateContract)

		// Stages (Bühnen)
		r.Get("/stages", h.ListStages)
		r.Post("/stages", h.CreateStage)
		r.Patch("/stages/{id}/stats", h.UpdateStageStats)

		// Stage participants
		r.Post("/stages/{id}/participants", h.AddStageParticipant)
		r.Patch("/stages/{id}/participants/{participant_id}", h.UpdateStageParticipant)

		// Assign client to stage
		r.Post("/stages/{id}/assign-client", h.AssignClientToStage)

		// Cashflow
		r.Get("/cashflow/forecast", h.CashflowForecast)

		// App settings
		r.Get("/settings", h.ListSettings)
		r.Get("/settings/{key}", h.GetSetting)
		r.Put("/settings/{key}", h.UpsertSetting)

	})

	return r
}
