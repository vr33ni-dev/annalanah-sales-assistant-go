package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/lib/pq"
)

func (s *PostgresStore) SyncClientCompletedAt(ctx context.Context, clientID int, closed *bool, completedAt *string) error {
	if closed != nil && !*closed {
		if _, err := s.db.ExecContext(ctx, `UPDATE clients SET completed_at = NULL WHERE id = $1`, clientID); err != nil {
			return err
		}
	}
	if closed != nil && *closed && completedAt != nil {
		if _, err := s.db.ExecContext(ctx, `UPDATE clients SET completed_at = $1::date WHERE id = $2`, *completedAt, clientID); err != nil {
			return err
		}
	}
	return nil
}

func (s *PostgresStore) SyncClientStatusFromSales(ctx context.Context, salesProcessID int) error {
	_, err := s.db.ExecContext(ctx, `
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

func (s *PostgresStore) ValidateClientCompletedAt(ctx context.Context, clientID int, completedAt *time.Time) error {
	if completedAt == nil {
		return nil
	}

	completedDay := completedAt.UTC().Truncate(24 * time.Hour)

	var clientCreatedAt sql.NullTime
	if err := s.db.QueryRowContext(ctx, `SELECT created_at FROM clients WHERE id = $1`, clientID).Scan(&clientCreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("client not found")
		}
		return err
	}
	if clientCreatedAt.Valid {
		createdDay := clientCreatedAt.Time.UTC().Truncate(24 * time.Hour)
		if completedDay.Before(createdDay) {
			return fmt.Errorf("completed_at cannot be before client creation date")
		}
	}

	var followUpDate sql.NullTime
	if err := s.db.QueryRowContext(ctx, `
		SELECT follow_up_date
		FROM sales_process
		WHERE client_id = $1
		ORDER BY id DESC
		LIMIT 1
	`, clientID).Scan(&followUpDate); err != nil && err != sql.ErrNoRows {
		return err
	}
	if followUpDate.Valid {
		followUpDay := followUpDate.Time.UTC().Truncate(24 * time.Hour)
		if completedDay.Before(followUpDay) {
			return fmt.Errorf("completed_at cannot be before follow_up_date")
		}
	}

	return nil
}

func (s *PostgresStore) InsertClient(ctx context.Context, name, email, phone, source string, sourceStageID *int, status string) (int, error) {
	var id int
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO clients (name, email, phone, source, source_stage_id, status)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		name, email, phone, source, sourceStageID, status,
	).Scan(&id)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" && pqErr.Constraint == "unique_client_email" {
			return 0, ErrDuplicateEmail
		}
		return 0, err
	}
	return id, nil
}

func (s *PostgresStore) DeleteClientWithLeadReset(ctx context.Context, clientID int) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		UPDATE leads
		SET converted = FALSE,
		    converted_at = NULL,
		    converted_client_id = NULL
		WHERE converted_client_id = $1
	`, clientID); err != nil {
		return false, fmt.Errorf("failed to reset linked leads: %w", err)
	}

	result, err := tx.ExecContext(ctx, `DELETE FROM clients WHERE id = $1`, clientID)
	if err != nil {
		return false, fmt.Errorf("failed to delete client: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return false, nil
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("failed to commit: %w", err)
	}
	return true, nil
}

func (s *PostgresStore) GetClientCompletedAt(ctx context.Context, clientID int) (*time.Time, error) {
	var t sql.NullTime
	if err := s.db.QueryRowContext(ctx, `SELECT completed_at FROM clients WHERE id = $1`, clientID).Scan(&t); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if !t.Valid {
		return nil, nil
	}
	return &t.Time, nil
}

func (s *PostgresStore) UpdateClientFields(ctx context.Context, id int, name, email, phone, source, status string, sourceStageID *int, sourceStageIDSet bool, completedAt *time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE clients
		SET name = COALESCE($1, name),
			email = COALESCE($2, email),
			phone = COALESCE($3, phone),
			source = COALESCE($4, source),
			source_stage_id = CASE WHEN $5 THEN $6::int ELSE source_stage_id END,
			status = COALESCE($7, status),
			completed_at = $8
		WHERE id = $9
	`,
		nullStr(name),
		nullStr(email),
		nullStr(phone),
		nullStr(source),
		sourceStageIDSet,
		nullInt(sourceStageID),
		nullStr(status),
		nullTime(completedAt),
		id,
	)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return ErrDuplicateEmail
		}
	}
	return err
}

func (s *PostgresStore) ClearClientSalesProcessStageID(ctx context.Context, clientID int) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sales_process SET stage_id = NULL WHERE client_id = $1`, clientID)
	return err
}

func (s *PostgresStore) SyncLeadFromClient(ctx context.Context, clientID int, name, email, phone, source string, sourceStageID *int, sourceStageIDSet bool) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE leads
		SET
			name            = COALESCE(NULLIF($1,''), name),
			email           = COALESCE(NULLIF($2,''), email),
			phone           = COALESCE(NULLIF($3,''), phone),
			source          = COALESCE(NULLIF($4,''), source),
			source_stage_id = CASE WHEN $5 THEN $6::int ELSE source_stage_id END
		WHERE converted_client_id = $7
	`,
		name, email, phone, source,
		sourceStageIDSet, nullInt(sourceStageID),
		clientID,
	)
	return err
}
