package api

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
)

func NewRouter(db *sql.DB) *chi.Mux {
	h := &Handler{DB: db}
	r := chi.NewRouter()

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

		// Optional:
		// Stage participants (individuals in a stage)
		r.Post("/stages/{id}/participants", h.AddStageParticipant)
		r.Patch("/stages/{id}/participants/{participant_id}", h.UpdateStageParticipant)

		// Assign client to stage after a sales process has been initialized and a client entry has been created
		r.Post("/stages/{id}/assign-client", h.AssignClientToStage)
	})

	return r
}
