package api

import (
	"context"
	"errors"
	"time"

	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/store"
)

var errInvalidContractStartDate = errors.New("invalid contract_start_date")

func (h *Handler) ensureContractForClosedSales(ctx context.Context, salesProcessID, clientID int, req SalesProcessUpdateRequest) error {
	if req.Closed == nil || !*req.Closed ||
		req.Revenue == nil ||
		req.ContractDurationMonths == nil || *req.ContractDurationMonths <= 0 ||
		req.ContractStartDate == nil || req.ContractFrequency == nil {
		return nil
	}

	sd, err := time.Parse("2006-01-02", *req.ContractStartDate)
	if err != nil {
		return errInvalidContractStartDate
	}

	notifyData, err := h.store.EnsureContractForClosedSales(ctx, salesProcessID, clientID, store.EnsureContractInput{
		Closed:                 req.Closed,
		Revenue:                req.Revenue,
		ContractDurationMonths: req.ContractDurationMonths,
		ContractStartDate:      req.ContractStartDate,
		ContractFrequency:      req.ContractFrequency,
	})
	if err != nil {
		return err
	}

	if notifyData != nil {
		spID := salesProcessID
		h.notifyWithContractData(notifyData, clientID, *req.Revenue, sd, &spID)
	}

	return nil
}
