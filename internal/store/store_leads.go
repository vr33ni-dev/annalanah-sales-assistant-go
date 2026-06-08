package store

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/domain"
)

func (s *PostgresStore) ListLeads(ctx context.Context) ([]domain.Lead, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT l.id, l.name, l.email, l.phone, l.source,
		       l.source_stage_id, st.name AS source_stage_name,
		       l.converted, l.created_at
		FROM leads l
		LEFT JOIN stages st ON st.id = l.source_stage_id
		ORDER BY l.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Lead
	for rows.Next() {
		var l domain.Lead
		var email, phone sql.NullString
		var stageName sql.NullString
		var stageID sql.NullInt64
		var createdAt sql.NullTime
		if err := rows.Scan(&l.ID, &l.Name, &email, &phone, &l.Source,
			&stageID, &stageName, &l.Converted, &createdAt); err != nil {
			return nil, err
		}
		if email.Valid {
			l.Email = email.String
		}
		if phone.Valid {
			l.Phone = phone.String
		}
		if stageID.Valid {
			v := int(stageID.Int64)
			l.SourceStageID = &v
		}
		if stageName.Valid {
			l.SourceStageName = &stageName.String
		}
		if createdAt.Valid {
			v := createdAt.Time.Format(time.RFC3339)
			l.CreatedAt = &v
		}
		out = append(out, l)
	}
	if out == nil {
		out = []domain.Lead{}
	}
	return out, rows.Err()
}

// CreateLead inserts a new lead. Returns (lead, created, error).
// created=false if the email already existed; in that case the existing lead is returned.
func (s *PostgresStore) CreateLead(ctx context.Context, name string, email, phone *string, source string, stageID *int) (domain.Lead, bool, error) {
	var lr domain.Lead
	var createdAt sql.NullTime

	err := s.db.QueryRowContext(ctx,
		`INSERT INTO leads (name, email, phone, source, source_stage_id)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at`,
		name, email, phone, source, stageID,
	).Scan(&lr.ID, &createdAt)

	if err != nil {
		if pgErr, ok := err.(*pq.Error); ok && pgErr.Code == "23505" {
			// Duplicate email: fetch existing lead
			if email == nil || *email == "" {
				return domain.Lead{}, false, err
			}
			row := s.db.QueryRowContext(ctx, `
				SELECT l.id, l.name, l.email, l.phone, l.source,
				       l.source_stage_id, COALESCE(st.name, '') AS source_stage_name,
				       l.converted, l.created_at
				FROM leads l
				LEFT JOIN stages st ON st.id = l.source_stage_id
				WHERE LOWER(l.email) = LOWER($1) LIMIT 1`, *email)
			var lEmail, lPhone sql.NullString
			var lStageID sql.NullInt64
			var lStageName sql.NullString
			var lCreatedAt sql.NullTime
			if scanErr := row.Scan(&lr.ID, &lr.Name, &lEmail, &lPhone, &lr.Source,
				&lStageID, &lStageName, &lr.Converted, &lCreatedAt); scanErr != nil {
				return domain.Lead{}, false, scanErr
			}
			if lEmail.Valid {
				lr.Email = lEmail.String
			}
			if lPhone.Valid {
				lr.Phone = lPhone.String
			}
			if lStageID.Valid {
				v := int(lStageID.Int64)
				lr.SourceStageID = &v
			}
			if lStageName.Valid && lStageName.String != "" {
				lr.SourceStageName = &lStageName.String
			}
			if lCreatedAt.Valid {
				v := lCreatedAt.Time.Format(time.RFC3339)
				lr.CreatedAt = &v
			}
			return lr, true, nil
		}
		return domain.Lead{}, false, err
	}

	lr.Name = name
	if email != nil {
		lr.Email = *email
	}
	if phone != nil {
		lr.Phone = *phone
	}
	lr.Source = source
	lr.SourceStageID = stageID
	if createdAt.Valid {
		v := createdAt.Time.Format(time.RFC3339)
		lr.CreatedAt = &v
	}
	return lr, false, nil
}

func (s *PostgresStore) UpdateLead(ctx context.Context, id int, name, email, phone, source *string, stageID *int) (domain.Lead, error) {
	var newStage sql.NullInt64
	if stageID != nil {
		newStage = sql.NullInt64{Int64: int64(*stageID), Valid: true}
	}

	row := s.db.QueryRowContext(ctx, `
		UPDATE leads SET
			name = COALESCE($1, name),
			email = COALESCE($2, email),
			phone = COALESCE($3, phone),
			source = COALESCE($4, source),
			source_stage_id = COALESCE($5, source_stage_id)
		WHERE id = $6
		RETURNING id, name, email, phone, source, source_stage_id,
		          COALESCE((SELECT name FROM stages WHERE id = source_stage_id), '') AS source_stage_name,
		          created_at`,
		name, email, phone, source, newStage, id)

	var lr domain.Lead
	var lEmail, lPhone sql.NullString
	var lStageID sql.NullInt64
	var lStageName sql.NullString
	var createdAt sql.NullTime

	if err := row.Scan(&lr.ID, &lr.Name, &lEmail, &lPhone, &lr.Source, &lStageID, &lStageName, &createdAt); err != nil {
		return domain.Lead{}, err
	}
	if lEmail.Valid {
		lr.Email = lEmail.String
	}
	if lPhone.Valid {
		lr.Phone = lPhone.String
	}
	if lStageID.Valid {
		v := int(lStageID.Int64)
		lr.SourceStageID = &v
	}
	if lStageName.Valid && lStageName.String != "" {
		lr.SourceStageName = &lStageName.String
	}
	if createdAt.Valid {
		v := createdAt.Time.Format(time.RFC3339)
		lr.CreatedAt = &v
	}
	return lr, nil
}

func (s *PostgresStore) DeleteLead(ctx context.Context, id int) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM leads WHERE id = $1`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) ConvertLead(ctx context.Context, leadID int) (clientID, salesProcessID int, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()

	// Load lead
	var name string
	var email, phone sql.NullString
	var source string
	var sourceStage sql.NullInt64
	if err := tx.QueryRowContext(ctx,
		`SELECT name, email, phone, source, source_stage_id FROM leads WHERE id = $1`, leadID,
	).Scan(&name, &email, &phone, &source, &sourceStage); err != nil {
		if err == sql.ErrNoRows {
			return 0, 0, ErrNotFound
		}
		return 0, 0, err
	}

	emailPtr := nullStringToPtr(email)
	phonePtr := nullStringToPtr(phone)
	var stagePtr *int
	if sourceStage.Valid {
		v := int(sourceStage.Int64)
		stagePtr = &v
	}

	// Create or reuse client
	stage := "follow_up"
	clientStatus := "follow_up_scheduled"

	if emailPtr != nil && *emailPtr != "" {
		if err := tx.QueryRowContext(ctx,
			`SELECT id FROM clients WHERE LOWER(email) = LOWER($1)`, *emailPtr,
		).Scan(&clientID); err == sql.ErrNoRows {
			if err := tx.QueryRowContext(ctx,
				`INSERT INTO clients (name, email, phone, source, source_stage_id, status)
				 VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
				name, emailPtr, phonePtr, source, stagePtr, clientStatus,
			).Scan(&clientID); err != nil {
				return 0, 0, err
			}
		} else if err != nil {
			return 0, 0, err
		}
	} else {
		if err := tx.QueryRowContext(ctx,
			`INSERT INTO clients (name, phone, source, source_stage_id, status)
			 VALUES ($1,$2,$3,$4,$5) RETURNING id`,
			name, phonePtr, source, stagePtr, clientStatus,
		).Scan(&clientID); err != nil {
			return 0, 0, err
		}
	}

	// Create or reuse sales_process
	err = tx.QueryRowContext(ctx, `
		INSERT INTO sales_process (client_id, stage, stage_id, lead_id)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (client_id) DO NOTHING
		RETURNING id`,
		clientID, stage, stagePtr, leadID,
	).Scan(&salesProcessID)
	if err == sql.ErrNoRows {
		if err := tx.QueryRowContext(ctx,
			`SELECT id FROM sales_process WHERE client_id = $1`, clientID,
		).Scan(&salesProcessID); err != nil {
			return clientID, 0, err
		}
	} else if err != nil {
		// Check if it's a unique_client_email conflict
		if pgErr, ok := err.(*pq.Error); ok && pgErr.Code == "23505" {
			// Try to find existing client by email
			if emailPtr != nil && strings.TrimSpace(*emailPtr) != "" {
				if lookupErr := s.db.QueryRowContext(ctx,
					`SELECT id FROM clients WHERE email = $1`, *emailPtr,
				).Scan(&clientID); lookupErr != nil {
					return 0, 0, err
				}
				// Try to create/find sales_process for existing client
				if spErr := s.db.QueryRowContext(ctx,
					`INSERT INTO sales_process (client_id, stage, stage_id, lead_id)
					 VALUES ($1, $2, $3, $4) RETURNING id`,
					clientID, stage, stagePtr, leadID,
				).Scan(&salesProcessID); spErr != nil {
					if pgErr2, ok2 := spErr.(*pq.Error); ok2 && pgErr2.Code == "23505" {
						if err := s.db.QueryRowContext(ctx,
							`SELECT id FROM sales_process WHERE client_id = $1`, clientID,
						).Scan(&salesProcessID); err != nil {
							return clientID, 0, err
						}
					} else {
						return clientID, 0, spErr
					}
				}
				return clientID, salesProcessID, nil
			}
		}
		return 0, 0, err
	}

	// Mark lead as converted
	if _, err := tx.ExecContext(ctx, `
		UPDATE leads SET converted = TRUE, converted_at = now(), converted_client_id = $1
		WHERE id = $2`, clientID, leadID); err != nil {
		return 0, 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return clientID, salesProcessID, nil
}
