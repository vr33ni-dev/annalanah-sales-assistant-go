package api

import (
	"database/sql"
	"net/http"

	"github.com/go-chi/chi/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
)

func NewRouterWithConfig(db *sql.DB, cfg *Config) *chi.Mux {
	h := &Handler{DB: db}
	r := chi.NewRouter()

	// Middlewares (order matters)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	origins := cfg.CORSOrigins
	if len(origins) == 0 {
		origins = []string{"http://localhost:5002"}
	}

	// CORS must be before routes
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   origins,
		AllowCredentials: true,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		MaxAge:           300,
	}))

	// Global OPTIONS (keep it)
	r.Options("/*", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	if err := h.InitAuth(); err != nil {
		panic(err)
	}

	// Public
	// r.Get("/health", h.health)
	h.MountAuthRoutes(r)

	// Protected API
	r.Route("/api", func(pr chi.Router) {
		pr.Use(h.RequireAuth)

		// 🔴 Add this so preflights to /api/... always return 204
		pr.Options("/*", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})

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

		// Stages
		pr.Get("/stages", h.ListStages)
		pr.Post("/stages", h.CreateStage)
		pr.Patch("/stages/{id}/stats", h.UpdateStageStats)

		// Stage participants
		pr.Post("/stages/{id}/participants", h.AddStageParticipant)
		pr.Patch("/stages/{id}/participants/{participant_id}", h.UpdateStageParticipant)

		// Assign client
		pr.Post("/stages/{id}/assign-client", h.AssignClientToStage)

		// Cashflow
		pr.Get("/cashflow/forecast", h.CashflowForecast)

		// Settings
		pr.Get("/settings", h.ListSettings)
		pr.Get("/settings/{key}", h.GetSetting)
		pr.Put("/settings/{key}", h.UpsertSetting)
	})

	return r
}
