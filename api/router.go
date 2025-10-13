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

	// Log requests (important for production diagnosis)
	r.Use(middleware.Logger)

	// Recover so panics don’t become 502s
	r.Use(middleware.Recoverer)

	// CORS FIRST
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		AllowCredentials: true,
		MaxAge:           300,
	}))
	r.Options("/*", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })

	// ✅ Init auth BEFORE mounting any route that uses it
	if err := h.InitAuth(); err != nil {
		panic(err) // or return nil / log.Fatal
	}

	// Public
	r.Get("/health", h.health)
	h.MountAuthRoutes(r)

	// Protected
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
