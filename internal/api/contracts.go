package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/domain"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/store"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/pkg/mailer"
)

type UpdateContractRequest struct {
	StartDate      string           `json:"start_date"`
	DurationMonths int              `json:"duration_months"`
	RevenueTotal   float64          `json:"revenue_total"`
	PaymentFreq    string           `json:"payment_frequency"`
	Comments       []domain.Comment `json:"comments,omitempty"`
}

type ContractResponse struct {
	ID                int                             `json:"id"`
	ClientID          int                             `json:"client_id"`
	ClientName        string                          `json:"client_name"`
	SalesProcessID    *int                            `json:"sales_process_id"`
	CreatedAt         *string                         `json:"created_at,omitempty"`
	UpdatedAt         *string                         `json:"updated_at,omitempty"`
	StartDate         string                          `json:"start_date"`
	EndDate           *string                         `json:"end_date,omitempty"`
	DurationMonths    int                             `json:"duration_months"`
	RevenueTotal      float64                         `json:"revenue_total"`
	PaymentFreq       string                          `json:"payment_frequency"`
	BaseMonthlyAmount float64                         `json:"base_monthly_amount"`
	NextDueDate       *string                         `json:"next_due_date,omitempty"`
	Source            string                          `json:"source"`
	MonetaryMode      string                          `json:"monetary_mode"`
	Cashflow          []ContractCashflowEntryResponse `json:"cashflow,omitempty"`
	Comments          []CommentResponse               `json:"comments,omitempty"`
	Chain             []ContractResponse              `json:"chain,omitempty"`
}

type ContractCashflowEntryResponse struct {
	ID           int     `json:"id"`
	ContractID   int     `json:"contract_id"`
	DueDate      *string `json:"due_date"`
	Amount       float64 `json:"amount"`
	Status       string  `json:"status"`
	UpdatedAt    *string `json:"updated_at,omitempty"`
	MonetaryMode string  `json:"monetary_mode"`
}

func rowToContractResponse(cr domain.ContractRow, mwstRate float64) ContractResponse {
	resp := ContractResponse{
		ID:                cr.ID,
		ClientID:          cr.ClientID,
		ClientName:        cr.ClientName,
		SalesProcessID:    cr.SalesProcessID,
		StartDate:         cr.StartDate,
		EndDate:           cr.EndDate,
		CreatedAt:         cr.CreatedAt,
		UpdatedAt:         cr.UpdatedAt,
		DurationMonths:    cr.DurationMonths,
		RevenueTotal:      netFromGross(cr.RevenueBrutto, mwstRate),
		PaymentFreq:       cr.PaymentFreq,
		BaseMonthlyAmount: netFromGross(cr.BaseMonthlyBrutto, mwstRate),
		NextDueDate:       cr.NextDueDate,
		Source:            cr.Source,
		MonetaryMode:      monetaryModeNetto,
	}

	if len(cr.Comments) > 0 {
		resp.Comments = make([]CommentResponse, len(cr.Comments))
		for i, c := range cr.Comments {
			resp.Comments[i] = CommentResponse{
				ID:         c.ID,
				EntityType: c.EntityType,
				EntityID:   c.EntityID,
				Author:     c.Author,
				Body:       c.Body,
				Metadata:   c.Metadata,
				CreatedAt:  c.CreatedAt.Format(time.RFC3339),
				UpdatedAt:  c.UpdatedAt.Format(time.RFC3339),
			}
		}
	} else {
		resp.Comments = []CommentResponse{}
	}

	if len(cr.CashflowEntries) > 0 {
		resp.Cashflow = make([]ContractCashflowEntryResponse, len(cr.CashflowEntries))
		for i, ce := range cr.CashflowEntries {
			resp.Cashflow[i] = ContractCashflowEntryResponse{
				ID:           ce.ID,
				ContractID:   ce.ContractID,
				DueDate:      ce.DueDate,
				Amount:       ce.Amount,
				Status:       ce.Status,
				UpdatedAt:    ce.UpdatedAt,
				MonetaryMode: monetaryModeBrutto,
			}
		}
	} else {
		resp.Cashflow = []ContractCashflowEntryResponse{}
	}

	if len(cr.Chain) > 0 {
		resp.Chain = make([]ContractResponse, len(cr.Chain))
		for i, cx := range cr.Chain {
			resp.Chain[i] = rowToContractResponse(cx, mwstRate)
		}
	} else {
		resp.Chain = []ContractResponse{}
	}

	return resp
}

func normalizePaymentFrequency(paymentFreq string, durationMonths int) (string, error) {
	pf := strings.ToLower(strings.TrimSpace(paymentFreq))
	switch pf {
	case "monthly", "bi-monthly", "quarterly", "one-time", "bi-yearly":
	default:
		return "", fmt.Errorf("invalid payment_frequency (allowed: monthly, bi-monthly, quarterly, one-time, bi-yearly)")
	}
	if pf == "bi-yearly" && durationMonths < 12 {
		return "", fmt.Errorf("bi-yearly payment frequency requires duration_months >= 12")
	}
	return pf, nil
}

// notifyWithContractData sends the new-contract email using pre-loaded notify data.
// Use this when the caller already has ContractNotifyData (avoids a redundant DB fetch).
func (h *Handler) notifyWithContractData(data *domain.ContractNotifyData, clientID int, revenue float64, startDate time.Time, salesProcessID *int) {
	if salesProcessID == nil || data == nil {
		return
	}
	notifyTo := h.getTextSetting("new_contract_notify_email", "")
	if notifyTo == "" {
		notifyTo = os.Getenv("NEW_CONTRACT_NOTIFY_EMAIL")
	}
	if notifyTo == "" {
		return
	}
	clientName := fmt.Sprintf("Kunde #%d", clientID)
	if data.ClientName != "" {
		clientName = data.ClientName
	}
	go func() {
		if err := mailer.SendNewContractNotification(
			notifyTo, clientName, startDate.Format("2006-01-02"),
			data.ClosureDate, data.Source, data.StageName, revenue, data.NextDueDate,
		); err != nil {
			fmt.Printf("failed to send new contract notification: %v\n", err)
		}
	}()
}

func (h *Handler) notifyNewContractAsync(contractID, clientID int, revenue float64, startDate time.Time, salesProcessID *int) {
	if salesProcessID == nil {
		return
	}
	notifyTo := h.getTextSetting("new_contract_notify_email", "")
	if notifyTo == "" {
		notifyTo = os.Getenv("NEW_CONTRACT_NOTIFY_EMAIL")
	}
	if notifyTo == "" {
		return
	}

	data, err := h.store.GetContractNotifyData(context.Background(), contractID)
	clientName := fmt.Sprintf("Kunde #%d", clientID)
	closureDate, source, stageName, nextDueDate := "", "", "", ""
	if err == nil {
		if data.ClientName != "" {
			clientName = data.ClientName
		}
		closureDate = data.ClosureDate
		source = data.Source
		stageName = data.StageName
		nextDueDate = data.NextDueDate
	}

	go func() {
		if err := mailer.SendNewContractNotification(
			notifyTo, clientName, startDate.Format("2006-01-02"),
			closureDate, source, stageName, revenue, nextDueDate,
		); err != nil {
			fmt.Printf("failed to send new contract notification: %v\n", err)
		}
	}()
}

// GET /api/contracts
func (h *Handler) ListContracts(w http.ResponseWriter, r *http.Request) {
	includeExpired := strings.EqualFold(r.URL.Query().Get("include_expired"), "true")
	compact := strings.EqualFold(r.URL.Query().Get("compact"), "true")
	includeComments := !compact && !strings.EqualFold(r.URL.Query().Get("include_comments"), "false")
	includeCashflow := !strings.EqualFold(r.URL.Query().Get("include_cashflow"), "false")
	mwstRate := defaultMwstRate

	contracts, err := h.store.ListContracts(r.Context(), includeExpired, includeComments, includeCashflow)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	out := make([]ContractResponse, len(contracts))
	for i, cr := range contracts {
		out[i] = rowToContractResponse(cr, mwstRate)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// GET /api/contracts/{id}
func (h *Handler) GetContract(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid contract id", http.StatusBadRequest)
		return
	}

	cr, err := h.store.GetContractByID(r.Context(), id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	mwstRate := defaultMwstRate
	out := rowToContractResponse(cr, mwstRate)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// GET /api/contracts/{id}/cashflow
func (h *Handler) ListContractCashflowEntries(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid contract id", http.StatusBadRequest)
		return
	}

	entries, err := h.store.GetContractCashflow(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	out := make([]ContractCashflowEntryResponse, len(entries))
	for i, ce := range entries {
		out[i] = ContractCashflowEntryResponse{
			ID:           ce.ID,
			ContractID:   ce.ContractID,
			DueDate:      ce.DueDate,
			Amount:       ce.Amount,
			Status:       ce.Status,
			UpdatedAt:    ce.UpdatedAt,
			MonetaryMode: monetaryModeBrutto,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// POST /api/contracts
func (h *Handler) CreateContract(w http.ResponseWriter, r *http.Request) {
	var c domain.Contract

	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	pf, err := normalizePaymentFrequency(c.PaymentFreq, c.DurationMonths)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	c.PaymentFreq = pf

	sd, err := time.Parse("2006-01-02", c.StartDate)
	if err != nil {
		http.Error(w, "invalid start_date (expected YYYY-MM-DD)", http.StatusBadRequest)
		return
	}

	var ed *time.Time
	if c.EndDate != nil && *c.EndDate != "" {
		parsedEnd, err := time.Parse("2006-01-02", *c.EndDate)
		if err != nil {
			http.Error(w, "invalid end_date (expected YYYY-MM-DD)", http.StatusBadRequest)
			return
		}
		if parsedEnd.Before(sd) {
			http.Error(w, "end_date cannot be before start_date", http.StatusBadRequest)
			return
		}
		ed = &parsedEnd
	}

	contractID, createdAt, err := h.store.CreateContract(r.Context(), store.ContractCreateInput{
		ClientID:         c.ClientID,
		SalesProcessID:   c.SalesProcessID,
		StartDate:        sd,
		EndDate:          ed,
		DurationMonths:   c.DurationMonths,
		RevenueTotal:     c.RevenueTotal,
		PaymentFreq:      c.PaymentFreq,
		GenerateSchedule: true,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	c.ID = contractID
	c.CreatedAt = createdAt

	if len(c.Comments) > 0 {
		_ = h.insertCommentsForEntity("contract", c.ID, c.ClientID, c.Comments)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(c)

	h.notifyNewContractAsync(c.ID, c.ClientID, c.RevenueTotal, sd, c.SalesProcessID)
}

// PATCH /api/contracts/{id}
func (h *Handler) UpdateContract(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid contract id", http.StatusBadRequest)
		return
	}

	var req UpdateContractRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	pf, err := normalizePaymentFrequency(req.PaymentFreq, req.DurationMonths)
	if err != nil {
		http.Error(w, "invalid payment_frequency", http.StatusBadRequest)
		return
	}
	req.PaymentFreq = pf

	sd, err := time.Parse("2006-01-02", req.StartDate)
	if err != nil {
		http.Error(w, "invalid start_date (expected YYYY-MM-DD)", http.StatusBadRequest)
		return
	}
	ed := sd.AddDate(0, req.DurationMonths, 0)

	if err := h.store.UpdateContract(r.Context(), id, sd, ed, req.DurationMonths, req.RevenueTotal, req.PaymentFreq); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if len(req.Comments) > 0 {
		contractClientID, err := h.store.GetContractClientID(r.Context(), id)
		if err == nil {
			_ = h.insertCommentsForEntity("contract", id, contractClientID, req.Comments)
		}
	}

	w.WriteHeader(http.StatusNoContent)
}
