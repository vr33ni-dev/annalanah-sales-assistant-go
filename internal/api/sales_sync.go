package api

import "context"

func (h *Handler) syncClientCompletedAtFromSales(ctx context.Context, clientID int, req SalesProcessUpdateRequest) error {
	return h.store.SyncClientCompletedAt(ctx, clientID, req.Closed, req.CompletedAt)
}

func (h *Handler) syncClientStatusFromSales(ctx context.Context, salesProcessID int) error {
	return h.store.SyncClientStatusFromSales(ctx, salesProcessID)
}
