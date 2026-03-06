package api

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var errInvalidContractStartDate = errors.New("invalid contract_start_date")

func (h *Handler) ensureContractForClosedSales(ctx context.Context, salesProcessID, clientID int, req SalesProcessUpdateRequest) error {
	if req.Closed == nil || !*req.Closed ||
		req.Revenue == nil ||
		req.ContractDurationMonths == nil || *req.ContractDurationMonths <= 0 ||
		req.ContractStartDate == nil || req.ContractFrequency == nil {
		return nil
	}

	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	sd, err := time.Parse("2006-01-02", *req.ContractStartDate)
	if err != nil {
		return errInvalidContractStartDate
	}

	var existingContractID int
	err = tx.QueryRowContext(ctx, `
		SELECT id
		FROM contracts
		WHERE sales_process_id = $1
		LIMIT 1
	`, salesProcessID).Scan(&existingContractID)
	if err != nil && err != sql.ErrNoRows {
		return err
	}

	var notifyContractID *int
	var notifyRevenue *float64
	var notifyStartDate *time.Time
	var notifySalesProcessID *int

	if err == sql.ErrNoRows {
		spID := salesProcessID
		contractID, _, err := h.createContractTx(ctx, tx, ContractCreateInput{
			ClientID:         clientID,
			SalesProcessID:   &spID,
			StartDate:        sd,
			DurationMonths:   *req.ContractDurationMonths,
			RevenueTotal:     *req.Revenue,
			PaymentFreq:      *req.ContractFrequency,
			GenerateSchedule: true,
		})
		if err != nil {
			return err
		}
		notifyContractID = &contractID
		notifyRevenue = req.Revenue
		notifyStartDate = &sd
		notifySalesProcessID = &spID
	}

	if _, err = tx.ExecContext(ctx, `
		UPDATE leads
		SET
			converted = TRUE,
			converted_at = now(),
			converted_client_id = $1
		WHERE id = (
			SELECT lead_id
			FROM sales_process
			WHERE id = $2
			  AND lead_id IS NOT NULL
		)
		  AND converted = FALSE
	`, clientID, salesProcessID); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	if notifyContractID != nil && notifyRevenue != nil && notifyStartDate != nil && notifySalesProcessID != nil {
		h.notifyNewContractAsync(*notifyContractID, clientID, *notifyRevenue, *notifyStartDate, notifySalesProcessID)
	}

	return nil
}