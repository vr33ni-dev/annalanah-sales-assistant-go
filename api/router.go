package api

import (
	"database/sql"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
)

func NewRouterWithConfig(db *sql.DB, cfg *Config) *chi.Mux {
	h := &Handler{DB: db}
	r := chi.NewRouter()

	// Initialize auth using cfg values inside InitAuth (read envs there or accept cfg)
	if err := h.InitAuth(); err != nil {
		panic(err)
	}

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.CORSOrigins, // e.g. ["http://localhost:5002","https://vr33ni-dev.github.io"]
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// public
	r.Get("/health", h.health)
	h.MountAuthRoutes(r) // /auth/google, /auth/google/callback, /api/me

	h.MountAuthRoutes(r) // /auth/google, /auth/google/callback, /api/me
	h.MountDevRoutes(r)

	// protected
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

func (h *Handler) health(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }
