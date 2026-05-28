// sales_sync.go — thin wrappers that keep the client record in sync after a sales update
// (completed_at and status derived from the sales process outcome).
package api

import "context"

func (h *Handler) syncClientCompletedAtFromSales(ctx context.Context, clientID int, req SalesProcessUpdateRequest) error {
	return h.store.SyncClientCompletedAt(ctx, clientID, req.Closed, req.CompletedAt)
}

func (h *Handler) syncClientStatusFromSales(ctx context.Context, salesProcessID int) error {
	return h.store.SyncClientStatusFromSales(ctx, salesProcessID)
}
