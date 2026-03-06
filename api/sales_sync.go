package api

import "context"

func (h *Handler) syncClientCompletedAtFromSales(ctx context.Context, clientID int, req SalesProcessUpdateRequest) error {
	// Keep clients.completed_at consistent with the sales process state.
	// - If closed is explicitly false -> clear completed_at.
	// - If closed is explicitly true and completed_at provided -> set it.
	if req.Closed != nil && !*req.Closed {
		if _, err := h.DB.ExecContext(ctx, `UPDATE clients SET completed_at = NULL WHERE id = $1`, clientID); err != nil {
			return err
		}
	}
	if req.Closed != nil && *req.Closed && req.CompletedAt != nil {
		if _, err := h.DB.ExecContext(ctx, `UPDATE clients SET completed_at = $1::date WHERE id = $2`, *req.CompletedAt, clientID); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) syncClientStatusFromSales(ctx context.Context, salesProcessID int) error {
	_, err := h.DB.ExecContext(ctx, `
	WITH s AS (
		SELECT client_id, stage, follow_up_result, closed, initial_contact_date
		FROM sales_process WHERE id = $1
	)
	  UPDATE clients c
	  SET status = CASE
	    WHEN (SELECT stage FROM s) = 'closed'
	         AND COALESCE((SELECT closed FROM s), FALSE) = TRUE
	      THEN 'active'
	    WHEN (SELECT stage FROM s) = 'lost'
	      THEN 'lost'
		WHEN (SELECT stage FROM s) = 'initial_contact'
			 AND (SELECT initial_contact_date FROM s) IS NOT NULL
			 AND (SELECT follow_up_result FROM s) IS NULL
		THEN 'initial_call_scheduled'
		WHEN (SELECT stage FROM s) = 'follow_up'
				 AND (SELECT follow_up_result FROM s) IS NULL
			THEN 'follow_up_scheduled'
	    WHEN (SELECT stage FROM s) = 'follow_up'
	         AND (SELECT follow_up_result FROM s) IS TRUE
	      THEN 'awaiting_response'
	    ELSE c.status
	  END
	  WHERE c.id = (SELECT client_id FROM s)
	`, salesProcessID)
	return err
}