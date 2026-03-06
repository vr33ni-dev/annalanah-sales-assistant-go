package api

import (
	"context"
	"database/sql"
)

func (h *Handler) shouldDeleteLostTemporaryClient(ctx context.Context, salesProcessID, clientID int) (bool, error) {
	var leadID sql.NullInt64
	if err := h.DB.QueryRowContext(ctx, `
		SELECT lead_id
		FROM sales_process
		WHERE id = $1
	`, salesProcessID).Scan(&leadID); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	if !leadID.Valid {
		return false, nil
	}

	var converted bool
	if err := h.DB.QueryRowContext(ctx, `SELECT converted FROM leads WHERE id = $1`, leadID.Int64).Scan(&converted); err != nil {
		return false, err
	}
	if converted {
		return false, nil
	}

	var hasContracts bool
	if err := h.DB.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM contracts WHERE client_id = $1)`, clientID).Scan(&hasContracts); err != nil {
		return false, err
	}

	return !hasContracts, nil
}

func (h *Handler) deleteTemporaryClientWithComments(ctx context.Context, clientID, salesProcessID int) error {
	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM comments
		WHERE (entity_type = 'client' AND entity_id = $1)
		   OR (entity_type = 'sales_process' AND entity_id = $2)
	`, clientID, salesProcessID); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM clients WHERE id = $1`, clientID); err != nil {
		return err
	}

	return tx.Commit()
}
