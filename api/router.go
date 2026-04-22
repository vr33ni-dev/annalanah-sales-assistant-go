package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
)

func NewRouterWithConfig(db *sql.DB, cfg *Config) *chi.Mux {
	h := &Handler{DB: db, Cfg: cfg}
	r := chi.NewRouter()

	// Middlewares (order matters)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// marker header to confirm requests hit Go backend
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("X-App", "go-backend")
			next.ServeHTTP(w, req)
		})
	})

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

	// Global OPTIONS
	r.Options("/*", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	if err := h.InitAuth(); err != nil {
		panic(err)
	}

	// Public
	r.Get("/health", h.health)
	h.MountAuthRoutes(r)

	// The auth routes are mounted by `MountAuthRoutes` (see api/auth.go) and
	// include the following useful endpoints for the frontend and debugging:
	//   - GET  /api/me         -> returns session or 401
	//   - GET  /api/user/me    -> convenience: returns user-shaped object even when unauthenticated
	//   - GET  /debug/session  -> shows cookies received by the server and parsed session (debug only)

	// ✅ Add this: make backend "/" respond 200 so health probes don't 405
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	r.Get("/debug/cookies", func(w http.ResponseWriter, r *http.Request) {
		type c struct{ Name, Value string }
		var list []c
		for _, ck := range r.Cookies() {
			list = append(list, c{ck.Name, ck.Value})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(list)
	})

	// Protected API
	r.Route("/api", func(pr chi.Router) {

		if strings.ToLower(cfg.AppEnv) != "local" {
			pr.Use(h.RequireAuth)
		}

		// Prevent caching of sensitive API responses
		pr.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Cache-Control", "no-store")
				next.ServeHTTP(w, r)
			})
		})

		// Preflights to /api/... always return 204
		pr.Options("/*", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})

		// Clients
		pr.Get("/clients", h.ListClients)
		pr.Post("/clients", h.CreateClient)
		pr.Patch("/clients/{id}", h.UpdateClient)
		pr.Delete("/clients/{id}", h.DeleteClient)

		pr.Get("/leads", h.ListLeads)
		pr.Post("/leads", h.CreateLead)
		pr.Patch("/leads/{id}", h.UpdateLead)
		pr.Post("/leads/{id}/convert", h.ConvertLead)
		pr.Delete("/leads/{id}", h.DeleteLead)

		// Sales processes
		pr.Get("/sales", h.ListSalesProcesses)
		pr.Patch("/sales/{id}", h.UpdateSalesProcess)
		pr.Post("/sales/start", h.StartSalesProcess)

		pr.Get("/sales/upsells/list", h.ListUpsellCategories)    // returns upsell_revenue as Netto
		pr.Get("/sales/upsells/analytics", h.GetUpsellAnalytics) // returns umsatz_sum+revenue_by_month as Netto

		// Dashboard KPIs (aggregated — replaces heavy frontend cross-referencing)
		pr.Get("/dashboard/kpis", h.GetDashboardKPIs)                  // returns revenue KPI fields as Netto
		pr.Get("/dashboard/monthly-kpis", h.GetMonthlyKPIs)            // returns monthly revenue as Netto
		pr.Get("/dashboard/contracts-in-range", h.GetContractsInRange) // returns revenue_netto per contract

		pr.Get("/sales/{id}/upsell", h.GetUpsellForSalesProcess) // returns upsell_revenue as Netto
		pr.Patch("/sales/{id}/upsell", h.CreateOrUpdateUpsell)   // schedule or update a single upsell

		// Contracts
		pr.Get("/contracts", h.ListContracts)         // response: revenue_total+base_monthly_amount are Netto
		pr.Post("/contracts", h.CreateContract)       // request payload monetary fields are Brutto
		pr.Get("/contracts/{id}", h.GetContract)      // response: revenue_total+base_monthly_amount are Netto
		pr.Patch("/contracts/{id}", h.UpdateContract) // request payload monetary fields are Brutto
		pr.Get("/contracts/{id}/cashflow", h.ListContractCashflowEntries)

		// Stages
		pr.Get("/stages", h.ListStages)
		pr.Post("/stages", h.CreateStage)
		pr.Delete("/stages/{id}", h.DeleteStage)
		pr.Patch("/stages/{id}/stats", h.UpdateStageStats)
		pr.Patch("/stages/{id}", h.UpdateStageInfo)

		// Stage participants
		pr.Get("/stages/{id}/participants", h.ListStageParticipants)
		pr.Post("/stages/{id}/participants", h.AddStageParticipant)
		pr.Patch("/stages/{id}/participants/{participant_id}", h.UpdateStageParticipant)
		pr.Delete("/stages/{id}/participants/{participant_id}", h.DeleteStageParticipant)

		// Assign client
		pr.Post("/stages/{id}/assign-client", h.AssignClientToStage)

		// Cashflow
		pr.Get("/cashflow/forecast", h.CashflowForecast)
		pr.Get("/cashflow/metrics", h.CashflowMetrics)
		pr.Get("/cashflow/entries", h.ListCashflowEntries)
		pr.Patch("/cashflow/entries/{id}/status", h.UpdateCashflowEntryStatus)

		// Exports
		pr.Get("/exports/raw/clients.csv", h.ExportRawClientsCSV)
		pr.Get("/exports/raw/contracts.csv", h.ExportRawContractsCSV)
		pr.Get("/exports/raw/cashflow_entries.csv", h.ExportRawCashflowEntriesCSV)
		pr.Get("/exports/aggregated/cashflow.csv", h.ExportAggregatedCashflowCSV)

		// Settings
		pr.Get("/settings", h.ListSettings)
		pr.Get("/settings/{key}", h.GetSetting)
		pr.Put("/settings/{key}", h.UpsertSetting)

		// NLQ (natural language query)
		pr.Post("/nlq", h.RunNLQ)

		// Comments
		// List comments for an entity. Required query parameters:
		//   - entity_type (string) e.g. "client" or "sales"
		//   - entity_id   (int)    e.g. 123
		// Example: GET /api/comments?entity_type=client&entity_id=123
		pr.Get("/comments", h.ListComments)
		pr.Post("/comments", h.CreateComment)
		pr.Patch("/comments/{id}", h.UpdateComment)
		pr.Delete("/comments/{id}", h.DeleteComment)

		// ---- Import routes (admin/internal only) ----
		pr.Route("/import", func(ir chi.Router) {
			ir.Post("/contracts", h.ImportContracts)
		})
	})

	return r
}
