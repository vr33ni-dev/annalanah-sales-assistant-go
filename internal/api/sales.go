package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/domain"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/store"
)

// What the API returns (GET /api/sales, PATCH /api/sales/{id})
type SalesProcessResponse struct {
	ID                 int               `json:"id"`
	ClientID           int               `json:"client_id"`
	ClientName         string            `json:"client_name"`
	ClientEmail        *string           `json:"client_email,omitempty"`
	ClientPhone        *string           `json:"client_phone,omitempty"`
	ClientSource       *string           `json:"client_source,omitempty"`
	CompletedAt        *string           `json:"completed_at"`
	Stage              string            `json:"stage"`
	CreatedAt          *string           `json:"created_at,omitempty"`
	UpdatedAt          *string           `json:"updated_at,omitempty"`
	InitialContactDate *string           `json:"initial_contact_date"`
	FollowUpDate       *string           `json:"follow_up_date"`
	FollowUpResult     *bool             `json:"follow_up_result"`
	Closed             *bool             `json:"closed"`
	Revenue            *float64          `json:"revenue"`
	StageID            *int              `json:"stage_id"`
	LeadID             *int              `json:"lead_id,omitempty"`
	Comments           []CommentResponse `json:"comments,omitempty"`
}

// What the API accepts (PATCH /api/sales/{id})
type SalesProcessUpdateRequest struct {
	InitialContactDate     *string          `json:"initial_contact_date,omitempty"`
	FollowUpDate           *string          `json:"follow_up_date,omitempty"`
	FollowUpResult         *bool            `json:"follow_up_result"`
	Closed                 *bool            `json:"closed"`
	Revenue                *float64         `json:"revenue"`
	StageID                *int             `json:"stage_id,omitempty"`
	ContractDurationMonths *int             `json:"contract_duration_months,omitempty"`
	ContractStartDate      *string          `json:"contract_start_date,omitempty"`
	ContractFrequency      *string          `json:"contract_frequency,omitempty"`
	CompletedAt            *string          `json:"completed_at,omitempty"`
	Comments               []domain.Comment `json:"comments,omitempty"`
}

func toSalesProcessResponse(sp domain.SalesProcess) SalesProcessResponse {
	comments := make([]CommentResponse, len(sp.Comments))
	for i, c := range sp.Comments {
		comments[i] = CommentResponse{
			ID:         c.ID,
			EntityType: c.EntityType,
			EntityID:   c.EntityID,
			ClientID:   c.ClientID,
			Author:     c.Author,
			Body:       c.Body,
			Metadata:   c.Metadata,
			CreatedAt:  c.CreatedAt.Format(time.RFC3339),
			UpdatedAt:  c.UpdatedAt.Format(time.RFC3339),
		}
	}
	return SalesProcessResponse{
		ID:                 sp.ID,
		ClientID:           sp.ClientID,
		ClientName:         sp.ClientName,
		ClientEmail:        sp.ClientEmail,
		ClientPhone:        sp.ClientPhone,
		ClientSource:       sp.ClientSource,
		CompletedAt:        sp.CompletedAt,
		Stage:              sp.Stage,
		InitialContactDate: sp.InitialContactDate,
		FollowUpDate:       sp.FollowUpDate,
		FollowUpResult:     sp.FollowUpResult,
		Closed:             sp.Closed,
		Revenue:            sp.Revenue,
		StageID:            sp.StageID,
		LeadID:             sp.LeadID,
		Comments:           comments,
	}
}

// GET /api/sales
func (h *Handler) ListSalesProcesses(w http.ResponseWriter, r *http.Request) {
	processes, err := h.store.ListSalesProcesses()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	out := make([]SalesProcessResponse, len(processes))
	for i, sp := range processes {
		out[i] = toSalesProcessResponse(sp)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// PATCH /api/sales/{id}
func (h *Handler) UpdateSalesProcess(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid sales process id", http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}

	var sp SalesProcessUpdateRequest
	if err := json.Unmarshal(body, &sp); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_, stageIDProvided := raw["stage_id"]

	// Normalize: no-show → force closed=false, clear contract fields
	if sp.FollowUpResult != nil && !*sp.FollowUpResult {
		f := false
		sp.Closed = &f
		sp.Revenue = nil
		sp.ContractDurationMonths = nil
		sp.ContractStartDate = nil
		sp.ContractFrequency = nil
		sp.CompletedAt = nil
	}

	// Validate: closed=true requires all contract fields
	if sp.Closed != nil && *sp.Closed {
		if sp.Revenue == nil ||
			sp.ContractDurationMonths == nil || *sp.ContractDurationMonths <= 0 ||
			sp.ContractStartDate == nil ||
			sp.ContractFrequency == nil ||
			(*sp.ContractFrequency != "monthly" && *sp.ContractFrequency != "bi-monthly" && *sp.ContractFrequency != "quarterly" && *sp.ContractFrequency != "one-time" && *sp.ContractFrequency != "bi-yearly") {
			http.Error(w, "cannot set closed=true without contract details (revenue, duration>0, start date, frequency)", http.StatusBadRequest)
			return
		}
	}

	// If closed=true but follow_up_result not provided, assume call happened
	if sp.Closed != nil && *sp.Closed && sp.FollowUpResult == nil {
		t := true
		sp.FollowUpResult = &t
	}

	rowsAffected, err := h.store.UpdateSalesProcess(r.Context(), id, store.SalesUpdateInput{
		InitialContactDate: sp.InitialContactDate,
		FollowUpDate:       sp.FollowUpDate,
		FollowUpResult:     sp.FollowUpResult,
		Closed:             sp.Closed,
		Revenue:            sp.Revenue,
		StageIDProvided:    stageIDProvided,
		StageID:            sp.StageID,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if rowsAffected == 0 {
		http.Error(w, "sales process not found", http.StatusNotFound)
		return
	}

	clientID, err := h.store.GetSalesProcessClientID(r.Context(), id)
	if err != nil {
		http.Error(w, "sales process not found", http.StatusNotFound)
		return
	}

	if err := h.syncClientCompletedAtFromSales(r.Context(), clientID, sp); err != nil {
		http.Error(w, "failed to sync completed_at: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := h.syncClientStatusFromSales(r.Context(), id); err != nil {
		http.Error(w, "failed to sync client status: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := h.ensureContractForClosedSales(r.Context(), id, clientID, sp); err != nil {
		if errors.Is(err, errInvalidContractStartDate) {
			http.Error(w, "invalid contract_start_date", http.StatusBadRequest)
			return
		}
		http.Error(w, "failed to ensure contract for closed sales: "+err.Error(), http.StatusInternalServerError)
		return
	}

	for _, c := range sp.Comments {
		body := strings.TrimSpace(c.Body)
		if body == "" {
			continue
		}
		author := c.Author
		if sess, ok := h.parseSession(r); ok {
			author = &sess.Name
		} else if author == nil {
			def := os.Getenv("DEFAULT_COMMENT_AUTHOR")
			if def == "" {
				def = "local-dev"
			}
			author = &def
		}
		metaBytes, _ := json.Marshal(c.Metadata)
		if err := h.store.InsertSalesProcessComment(r.Context(), id, clientID, author, body, metaBytes); err != nil {
			http.Error(w, "failed to save comment: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	loaded, err := h.store.GetSalesProcess(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(toSalesProcessResponse(loaded))
}

// POST /api/sales/start
type StartSalesProcessRequest struct {
	Name               string           `json:"name"`
	Email              string           `json:"email"`
	Phone              string           `json:"phone"`
	Source             string           `json:"source"`
	SourceStageID      *int             `json:"source_stage_id,omitempty"`
	InitialContactDate *string          `json:"initial_contact_date,omitempty"`
	FollowUpDate       *string          `json:"follow_up_date"`
	LeadID             *int             `json:"lead_id,omitempty"`
	MergeStrategy      *string          `json:"merge_strategy,omitempty"` // overwrite | keep_existing
	ClientID           *int             `json:"client_id,omitempty"`
	Comments           []domain.Comment `json:"comments,omitempty"`
}

type ClientResponse struct {
	ID            int               `json:"id"`
	Name          string            `json:"name"`
	Email         string            `json:"email"`
	Phone         string            `json:"phone"`
	Source        string            `json:"source"`
	SourceStageID *int              `json:"source_stage_id,omitempty"`
	Comments      []CommentResponse `json:"comments,omitempty"`
}

type SalesProcessSummary struct {
	ID                 int     `json:"id"`
	ClientID           int     `json:"client_id"`
	Stage              string  `json:"stage"`
	InitialContactDate *string `json:"initial_contact_date"`
	FollowUpDate       *string `json:"follow_up_date"`
	FollowUpResult     *bool   `json:"follow_up_result"`
	Closed             *bool   `json:"closed"`
	Revenue            *int    `json:"revenue"`
	StageID            *int    `json:"stage_id"`
	LeadID             *int    `json:"lead_id,omitempty"`
}

type StartSalesProcessResponse struct {
	SalesProcessID int                 `json:"sales_process_id"`
	Client         ClientResponse      `json:"client"`
	SalesProcess   SalesProcessSummary `json:"sales_process"`
}

func (h *Handler) StartSalesProcess(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var req StartSalesProcessRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.InitialContactDate == nil || strings.TrimSpace(*req.InitialContactDate) == "" {
		http.Error(w, "initial_contact_date is required", http.StatusBadRequest)
		return
	}

	if req.ClientID == nil && strings.TrimSpace(req.Email) == "" && strings.TrimSpace(req.Phone) == "" {
		http.Error(w, "email or phone is required", http.StatusBadRequest)
		return
	}

	foundLeadID, leadSource, leadStageID, err := h.store.ResolveLeadForSalesStart(ctx, req.LeadID, req.Email)
	if err != nil {
		http.Error(w, "lead resolution failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if leadSource != "" && strings.TrimSpace(req.Source) == "" {
		req.Source = leadSource
	}
	if leadStageID != nil && req.SourceStageID == nil {
		req.SourceStageID = leadStageID
	}

	var existingClientID *int
	var existing domain.ClientBasic
	if req.ClientID != nil {
		existingClientID = req.ClientID
		existing, err = h.store.GetExistingClientBasic(ctx, *req.ClientID)
		if err != nil {
			http.Error(w, "existing client resolution failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if existingClientID != nil {
		hasActiveContract, err := h.store.HasActiveContractForClient(ctx, *existingClientID)
		if err != nil {
			http.Error(w, "contract lookup failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if hasActiveContract {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":     "client_has_active_contract",
				"client_id": *existingClientID,
			})
			return
		}
	}

	conflicts := detectStartSalesConflicts(req, existingClientID, existing, foundLeadID)

	if existingClientID != nil && foundLeadID != nil && req.MergeStrategy == nil {
		ov := "overwrite"
		req.MergeStrategy = &ov
	}
	if existingClientID != nil && len(conflicts) > 0 && req.MergeStrategy == nil {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":               "client_exists",
			"client_id":           *existingClientID,
			"has_active_contract": false,
			"conflicts":           conflicts,
			"original_payload":    req,
		})
		return
	}

	clientID, salesID, stage, effectiveLeadID, err := h.store.RunStartSalesProcess(ctx, store.StartSalesInput{
		Name:               req.Name,
		Email:              req.Email,
		Phone:              req.Phone,
		Source:             req.Source,
		SourceStageID:      req.SourceStageID,
		InitialContactDate: req.InitialContactDate,
		FollowUpDate:       req.FollowUpDate,
		MergeStrategy:      req.MergeStrategy,
	}, existingClientID, foundLeadID)
	if err != nil {
		if isUniqueViolation(err, "unique_client_email") {
			writeJSONError(w, "Ein Kunde mit dieser E-Mail-Adresse existiert bereits. Bitte den bestehenden Kunden auswählen.", http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if len(req.Comments) > 0 {
		_ = h.insertCommentsForEntity("client", clientID, clientID, req.Comments)
	}

	resp, err := h.buildStartSalesProcessResponse(ctx, salesID, clientID, stage, req, effectiveLeadID)
	if err != nil {
		http.Error(w, "start sales response load failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}

type CreateUpsellRequest struct {
	UpsellDate             json.RawMessage `json:"upsell_date"`
	UpsellResult           *string         `json:"upsell_result,omitempty"`
	UpsellRevenue          *float64        `json:"upsell_revenue,omitempty"`
	ContractStartDate      *string         `json:"contract_start_date,omitempty"`
	ContractDurationMonths *int            `json:"contract_duration_months,omitempty"`
	ContractFrequency      *string         `json:"contract_frequency,omitempty"`
}

// GET /api/sales/{id}/upsell
func (h *Handler) GetUpsellForSalesProcess(w http.ResponseWriter, r *http.Request) {
	mwstRate := defaultMwstRate
	idStr := chi.URLParam(r, "id")
	salesID, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid sales process id", http.StatusBadRequest)
		return
	}

	list, err := h.store.GetUpsellForSalesProcess(r.Context(), salesID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	for i := range list {
		if list[i].UpsellRevenue != nil {
			v := netFromGross(*list[i].UpsellRevenue, mwstRate)
			list[i].UpsellRevenue = &v
		}
	}

	if list == nil {
		list = []domain.ContractUpsell{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(list)
}

// GET /api/sales/upsells/list
func (h *Handler) ListUpsellCategories(w http.ResponseWriter, r *http.Request) {
	mwstRate := defaultMwstRate
	q := r.URL.Query()

	var startDate, endDate *time.Time
	if v := q.Get("start_date"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			http.Error(w, "invalid start_date (expected YYYY-MM-DD)", http.StatusBadRequest)
			return
		}
		startDate = &t
	}
	if v := q.Get("end_date"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			http.Error(w, "invalid end_date (expected YYYY-MM-DD)", http.StatusBadRequest)
			return
		}
		endDate = &t
	}

	upsells, err := h.store.ListUpsells(r.Context(), startDate, endDate)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var scheduled, successful, unsuccessful []domain.ContractUpsell
	for i := range upsells {
		if upsells[i].UpsellRevenue != nil {
			v := netFromGross(*upsells[i].UpsellRevenue, mwstRate)
			upsells[i].UpsellRevenue = &v
		}
		switch {
		case upsells[i].UpsellResult == nil:
			scheduled = append(scheduled, upsells[i])
		case *upsells[i].UpsellResult == "verlaengerung":
			successful = append(successful, upsells[i])
		case *upsells[i].UpsellResult == "keine_verlaengerung":
			unsuccessful = append(unsuccessful, upsells[i])
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"scheduled":    scheduled,
		"successful":   successful,
		"unsuccessful": unsuccessful,
	})
}

// PATCH /api/sales/{id}/upsell
func (h *Handler) CreateOrUpdateUpsell(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	salesID, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid sales process id", http.StatusBadRequest)
		return
	}

	var req CreateUpsellRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	var resolvedUpsellDate *string
	upsellDateProvided := req.UpsellDate != nil
	if upsellDateProvided && string(req.UpsellDate) != "null" {
		var d string
		if err := json.Unmarshal(req.UpsellDate, &d); err != nil {
			http.Error(w, "invalid upsell_date", http.StatusBadRequest)
			return
		}
		resolvedUpsellDate = &d
	}

	if req.UpsellResult != nil {
		if *req.UpsellResult != "verlaengerung" && *req.UpsellResult != "keine_verlaengerung" {
			http.Error(w, "upsell_result must be 'verlaengerung' or 'keine_verlaengerung'", http.StatusBadRequest)
			return
		}
	}

	if req.UpsellResult != nil && *req.UpsellResult == "verlaengerung" && req.UpsellRevenue == nil {
		http.Error(w, "upsell_revenue required for verlängerung", http.StatusBadRequest)
		return
	}

	clientID, err := h.store.GetSalesProcessClientID(r.Context(), salesID)
	if err != nil {
		http.Error(w, "sales process not found", http.StatusNotFound)
		return
	}

	result, err := h.store.CreateOrUpdateUpsell(r.Context(), store.UpsellInput{
		SalesProcessID:         salesID,
		ClientID:               clientID,
		UpsellDateProvided:     upsellDateProvided,
		ResolvedUpsellDate:     resolvedUpsellDate,
		UpsellResult:           req.UpsellResult,
		UpsellRevenue:          req.UpsellRevenue,
		ContractStartDate:      req.ContractStartDate,
		ContractDurationMonths: req.ContractDurationMonths,
		ContractFrequency:      req.ContractFrequency,
	})
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "cannot create upsell") {
			http.Error(w, msg, http.StatusConflict)
			return
		}
		if strings.Contains(msg, "cannot be before") {
			http.Error(w, msg, http.StatusUnprocessableEntity)
			return
		}
		http.Error(w, msg, http.StatusInternalServerError)
		return
	}

	if result.NotifyContractID != nil && result.NotifyRevenue != nil && result.NotifyStartDate != nil && result.NotifySalesProcessID != nil {
		h.notifyNewContractAsync(*result.NotifyContractID, clientID, *result.NotifyRevenue, *result.NotifyStartDate, result.NotifySalesProcessID)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"upsell_id":       result.UpsellID,
		"updated":         result.Updated,
		"new_contract_id": result.NewContractID,
	})
}

// GET /api/sales/upsells/analytics
func (h *Handler) GetUpsellAnalytics(w http.ResponseWriter, r *http.Request) {
	mwstRate := defaultMwstRate
	q := r.URL.Query()

	var startDate, endDate *time.Time
	if v := q.Get("start_date"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			http.Error(w, "invalid start_date (expected YYYY-MM-DD)", http.StatusBadRequest)
			return
		}
		startDate = &t
	}
	if v := q.Get("end_date"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			http.Error(w, "invalid end_date (expected YYYY-MM-DD)", http.StatusBadRequest)
			return
		}
		endDate = &t
	}

	statsRaw, monthlyRevenues, err := h.store.GetUpsellAnalytics(r.Context(), startDate, endDate)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	umsatzNetto := netFromGross(statsRaw.UmsatzSumBrutto, mwstRate)
	for i := range monthlyRevenues {
		monthlyRevenues[i].Revenue = netFromGross(monthlyRevenues[i].Revenue, mwstRate)
	}

	type monthlyRevenueResponse struct {
		Month   string  `json:"month"`
		Revenue float64 `json:"revenue"`
	}
	revenueByMonth := make([]monthlyRevenueResponse, len(monthlyRevenues))
	for i, mr := range monthlyRevenues {
		revenueByMonth[i] = monthlyRevenueResponse{Month: mr.Month, Revenue: mr.Revenue}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"verlaengerung_count":       statsRaw.VerlaengerungCount,
		"keine_verlaengerung_count": statsRaw.KeineVerlaengerungCount,
		"scheduled_count":           statsRaw.ScheduledCount,
		"verlaengerungsquote":       statsRaw.Verlaengerungsquote,
		"umsatz_sum":                umsatzNetto,
		"revenue_by_month":          revenueByMonth,
	})
}
