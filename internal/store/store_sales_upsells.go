package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/domain"
)

func (s *PostgresStore) UpdateSalesProcess(ctx context.Context, id int, in SalesUpdateInput) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE sales_process
		SET
			initial_contact_date = COALESCE($1, initial_contact_date),
			follow_up_date       = COALESCE($2, follow_up_date),
			follow_up_result     = COALESCE($3, follow_up_result),
			closed               = COALESCE($4, closed),
			revenue              = CASE
				WHEN $4 IS TRUE  THEN $5
				WHEN $4 IS FALSE THEN NULL
				ELSE revenue
			END,
			stage_id = CASE WHEN $7 THEN $8::int ELSE stage_id END,
			stage = CASE
				WHEN COALESCE($3, follow_up_result) IS FALSE THEN 'lost'
				WHEN COALESCE($4, closed) IS TRUE  THEN 'closed'
				WHEN $4 IS NOT NULL AND $4 IS FALSE THEN 'lost'
				WHEN stage = 'lost' AND $4 IS NULL AND $3 IS NULL THEN 'lost'
				WHEN COALESCE($2, follow_up_date) IS NOT NULL THEN 'follow_up'
				WHEN COALESCE($1, initial_contact_date) IS NOT NULL THEN 'initial_contact'
				WHEN COALESCE($3, follow_up_result) IS TRUE THEN 'follow_up'
				ELSE 'follow_up'
			END
		WHERE id = $6`,
		in.InitialContactDate,
		in.FollowUpDate,
		in.FollowUpResult,
		in.Closed,
		in.Revenue,
		id,
		in.StageIDProvided,
		in.StageID,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *PostgresStore) GetSalesProcessClientID(ctx context.Context, salesProcessID int) (int, error) {
	var clientID int
	err := s.db.QueryRowContext(ctx,
		`SELECT client_id FROM sales_process WHERE id = $1`, salesProcessID,
	).Scan(&clientID)
	return clientID, err
}

func (s *PostgresStore) InsertSalesProcessComment(ctx context.Context, salesProcessID, clientID int, author *string, body string, metadata []byte) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO comments (entity_type, entity_id, client_id, author, body, metadata)
		 VALUES ('sales_process', $1, $2, $3, $4, $5::jsonb)`,
		salesProcessID, clientID, author, body, string(metadata),
	)
	return err
}

func scanUpsell(scanner interface {
	Scan(dest ...any) error
}, u *domain.ContractUpsell) error {
	var (
		upsellDate, createdAt, updatedAt              sql.NullTime
		upsellResult, contractFrequency               sql.NullString
		upsellRevenue                                 sql.NullFloat64
		previousContractID, newContractID             sql.NullInt64
		contractStartDate                             sql.NullTime
		contractDurationMonths                        sql.NullInt64
	)
	if err := scanner.Scan(
		&u.ID, &u.SalesProcessID, &u.ClientID,
		&upsellDate, &upsellResult, &upsellRevenue,
		&previousContractID, &newContractID,
		&createdAt, &updatedAt,
		&contractStartDate, &contractDurationMonths, &contractFrequency,
	); err != nil {
		return err
	}
	if upsellDate.Valid {
		v := upsellDate.Time.Format("2006-01-02")
		u.UpsellDate = &v
	}
	if upsellResult.Valid {
		u.UpsellResult = &upsellResult.String
	}
	if upsellRevenue.Valid {
		u.UpsellRevenue = &upsellRevenue.Float64
	}
	if previousContractID.Valid {
		v := int(previousContractID.Int64)
		u.PreviousContractID = &v
	}
	if newContractID.Valid {
		v := int(newContractID.Int64)
		u.NewContractID = &v
	}
	if createdAt.Valid {
		v := createdAt.Time.Format(time.RFC3339)
		u.CreatedAt = &v
	}
	if updatedAt.Valid {
		v := updatedAt.Time.Format(time.RFC3339)
		u.UpdatedAt = &v
	}
	if contractStartDate.Valid {
		u.ContractStartDate = &contractStartDate.Time
	}
	if contractDurationMonths.Valid {
		v := int(contractDurationMonths.Int64)
		u.ContractDurationMonths = &v
	}
	if contractFrequency.Valid {
		u.ContractFrequency = &contractFrequency.String
	}
	return nil
}

const upsellSelectSQL = `
SELECT cu.id, cu.sales_process_id, cu.client_id,
	cu.upsell_date, cu.upsell_result, cu.upsell_revenue,
	cu.previous_contract_id, cu.new_contract_id,
	cu.created_at, cu.updated_at,
	c.start_date AS contract_start_date,
	c.duration_months AS contract_duration_months,
	c.payment_frequency AS contract_frequency
FROM contract_upsells cu
LEFT JOIN contracts c ON c.id = cu.new_contract_id`

func (s *PostgresStore) GetUpsellForSalesProcess(ctx context.Context, salesProcessID int) ([]domain.ContractUpsell, error) {
	rows, err := s.db.QueryContext(ctx, upsellSelectSQL+`
WHERE cu.sales_process_id = $1
ORDER BY cu.upsell_date DESC NULLS LAST, cu.id DESC`, salesProcessID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.ContractUpsell
	for rows.Next() {
		var u domain.ContractUpsell
		if err := scanUpsell(rows, &u); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	if out == nil {
		out = []domain.ContractUpsell{}
	}
	return out, rows.Err()
}

func (s *PostgresStore) ListUpsells(ctx context.Context, startDate, endDate *time.Time) ([]domain.ContractUpsell, error) {
	query := upsellSelectSQL
	var args []any
	var where []string
	if startDate != nil {
		args = append(args, *startDate)
		where = append(where, fmt.Sprintf("cu.upsell_date >= $%d", len(args)))
	}
	if endDate != nil {
		args = append(args, *endDate)
		where = append(where, fmt.Sprintf("cu.upsell_date <= $%d", len(args)))
	}
	if len(where) > 0 {
		query += "\nWHERE " + strings.Join(where, " AND ")
	}
	query += "\nORDER BY cu.upsell_date DESC NULLS LAST, cu.id DESC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.ContractUpsell
	for rows.Next() {
		var u domain.ContractUpsell
		if err := scanUpsell(rows, &u); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	if out == nil {
		out = []domain.ContractUpsell{}
	}
	return out, rows.Err()
}

func (s *PostgresStore) CreateOrUpdateUpsell(ctx context.Context, in UpsellInput) (UpsellResult, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return UpsellResult{}, err
	}
	defer tx.Rollback()

	// Find existing open upsell
	var existingUpsellID *int
	err = tx.QueryRow(`
		SELECT id FROM contract_upsells
		WHERE sales_process_id = $1
		  AND (upsell_result IS NULL OR upsell_result IN ('offen', 'keine_verlaengerung'))
		ORDER BY updated_at DESC NULLS LAST, id DESC LIMIT 1`, in.SalesProcessID,
	).Scan(&existingUpsellID)
	if err == sql.ErrNoRows {
		existingUpsellID = nil
	} else if err != nil {
		return UpsellResult{}, err
	}

	// Determine previous contract
	var prevContractID *int
	_ = tx.QueryRow(`
		SELECT id FROM contracts WHERE client_id = $1
		ORDER BY start_date DESC, id DESC LIMIT 1`, in.ClientID,
	).Scan(&prevContractID)

	// Block if a blocking upsell exists
	if existingUpsellID == nil {
		var blockedCount int
		if err := tx.QueryRow(`
			SELECT COUNT(*) FROM contract_upsells u
			LEFT JOIN contracts c ON c.id = u.new_contract_id
			WHERE u.sales_process_id = $1
			  AND (
			    u.upsell_result IN ('keine_verlaengerung', 'offen')
			    OR u.upsell_result IS NULL
			    OR (u.upsell_result = 'verlaengerung' AND (u.new_contract_id IS NULL OR c.start_date > NOW()))
			  )`, in.SalesProcessID,
		).Scan(&blockedCount); err != nil {
			return UpsellResult{}, err
		}
		if blockedCount > 0 {
			return UpsellResult{}, fmt.Errorf("cannot create upsell: edit the existing upsell instead")
		}
	}

	var result UpsellResult
	result.Updated = existingUpsellID != nil

	// If verlaengerung: create new contract
	if in.UpsellResult != nil && *in.UpsellResult == "verlaengerung" {
		pf, err := normalizePaymentFrequency(*in.ContractFrequency, *in.ContractDurationMonths)
		if err != nil {
			return UpsellResult{}, err
		}
		sd, err := time.Parse("2006-01-02", *in.ContractStartDate)
		if err != nil {
			return UpsellResult{}, fmt.Errorf("invalid contract_start_date")
		}
		// Validate: new contract must not start before previous ends
		if prevContractID != nil {
			var prevEnd time.Time
			if scanErr := tx.QueryRow(`SELECT end_date FROM contracts WHERE id = $1`, *prevContractID).Scan(&prevEnd); scanErr == nil {
				if sd.Before(prevEnd) {
					return UpsellResult{}, fmt.Errorf("contract_start_date cannot be before the current contract's end date")
				}
			}
		}
		spID := in.SalesProcessID
		contractID, _, err := s.createContractTx(ctx, tx, ContractCreateInput{
			ClientID:         in.ClientID,
			SalesProcessID:   &spID,
			StartDate:        sd,
			DurationMonths:   *in.ContractDurationMonths,
			RevenueTotal:     *in.UpsellRevenue,
			PaymentFreq:      pf,
			GenerateSchedule: true,
		})
		if err != nil {
			return UpsellResult{}, fmt.Errorf("failed to create contract: %w", err)
		}
		result.NewContractID = &contractID
		result.NotifyContractID = &contractID
		result.NotifyRevenue = in.UpsellRevenue
		result.NotifyStartDate = &sd
		result.NotifySalesProcessID = &spID

		// Flip client back to active
		if _, err := tx.ExecContext(ctx, `UPDATE clients SET status = 'active' WHERE id = $1`, in.ClientID); err != nil {
			return UpsellResult{}, err
		}
	}

	// Insert or update upsell row
	if existingUpsellID == nil {
		err = tx.QueryRow(`
			INSERT INTO contract_upsells
			    (sales_process_id, client_id, upsell_date, upsell_result, upsell_revenue, previous_contract_id, new_contract_id)
			VALUES ($1,$2,$3,$4,$5,$6,$7)
			ON CONFLICT (sales_process_id) WHERE upsell_result IS NULL
			DO UPDATE SET
			    upsell_date     = COALESCE(EXCLUDED.upsell_date, contract_upsells.upsell_date),
			    upsell_result   = COALESCE(EXCLUDED.upsell_result, contract_upsells.upsell_result),
			    upsell_revenue  = COALESCE(EXCLUDED.upsell_revenue, contract_upsells.upsell_revenue),
			    new_contract_id = COALESCE(EXCLUDED.new_contract_id, contract_upsells.new_contract_id)
			RETURNING id`,
			in.SalesProcessID, in.ClientID, in.ResolvedUpsellDate, in.UpsellResult,
			in.UpsellRevenue, prevContractID, result.NewContractID,
		).Scan(&result.UpsellID)
	} else {
		err = tx.QueryRow(`
			UPDATE contract_upsells
			SET
			    upsell_date     = CASE WHEN $2 THEN $3 ELSE upsell_date END,
			    upsell_result   = COALESCE($4, upsell_result),
			    upsell_revenue  = COALESCE($5, upsell_revenue),
			    new_contract_id = COALESCE($6, new_contract_id)
			WHERE id = $1
			RETURNING id`,
			*existingUpsellID,
			in.UpsellDateProvided,
			in.ResolvedUpsellDate,
			in.UpsellResult,
			in.UpsellRevenue,
			result.NewContractID,
		).Scan(&result.UpsellID)
	}
	if err != nil {
		return UpsellResult{}, err
	}

	if err := tx.Commit(); err != nil {
		return UpsellResult{}, err
	}
	return result, nil
}

func (s *PostgresStore) GetUpsellAnalytics(ctx context.Context, startDate, endDate *time.Time) (UpsellStats, []MonthlyRevenue, error) {
	var stats UpsellStats
	var where []string
	var args []any
	if startDate != nil {
		args = append(args, *startDate)
		where = append(where, fmt.Sprintf("cu.upsell_date >= $%d", len(args)))
	}
	if endDate != nil {
		args = append(args, *endDate)
		where = append(where, fmt.Sprintf("cu.upsell_date <= $%d", len(args)))
	}
	whereSQL := ""
	if len(where) > 0 {
		whereSQL = "WHERE " + strings.Join(where, " AND ")
	}

	var vq sql.NullFloat64
	if err := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE upsell_result = 'verlaengerung'),
			COUNT(*) FILTER (WHERE upsell_result = 'keine_verlaengerung'),
			COUNT(*) FILTER (WHERE upsell_result IS NULL),
			ROUND(100.0 * COUNT(*) FILTER (WHERE upsell_result = 'verlaengerung')
			  / NULLIF(COUNT(*) FILTER (WHERE upsell_result IN ('verlaengerung','keine_verlaengerung')), 0), 1),
			COALESCE(SUM(upsell_revenue), 0)
		FROM contract_upsells cu `+whereSQL+`;`, args...,
	).Scan(&stats.VerlaengerungCount, &stats.KeineVerlaengerungCount, &stats.ScheduledCount, &vq, &stats.UmsatzSumBrutto); err != nil {
		return stats, nil, err
	}
	if vq.Valid {
		stats.Verlaengerungsquote = &vq.Float64
	}

	renewalWhere := append(where, "cu.upsell_result = 'verlaengerung'", "cu.upsell_date IS NOT NULL")
	renewalWhereSQL := "WHERE " + strings.Join(renewalWhere, " AND ")
	rows, err := s.db.QueryContext(ctx, `
		SELECT TO_CHAR(cu.upsell_date, 'YYYY-MM') AS month,
		       COALESCE(SUM(cu.upsell_revenue), 0) AS revenue
		FROM contract_upsells cu `+renewalWhereSQL+`
		GROUP BY month ORDER BY month`, args...)
	if err != nil {
		return stats, nil, err
	}
	defer rows.Close()

	var monthly []MonthlyRevenue
	for rows.Next() {
		var mr MonthlyRevenue
		if err := rows.Scan(&mr.Month, &mr.Revenue); err != nil {
			return stats, nil, err
		}
		monthly = append(monthly, mr)
	}
	return stats, monthly, rows.Err()
}

func (s *PostgresStore) HasActiveContractForClient(ctx context.Context, clientID int) (bool, error) {
	var has bool
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM contracts
			WHERE client_id = $1 AND end_date >= CURRENT_DATE
		)`, clientID,
	).Scan(&has)
	return has, err
}

func (s *PostgresStore) RunStartSalesProcess(ctx context.Context, in StartSalesInput, existingClientID, foundLeadID *int) (clientID, salesID int, stage string, effectiveLeadID *int, err error) {
	stage = "follow_up"
	if in.FollowUpDate != nil && strings.TrimSpace(*in.FollowUpDate) != "" {
		stage = "follow_up"
	} else if in.InitialContactDate != nil && strings.TrimSpace(*in.InitialContactDate) != "" {
		stage = "initial_contact"
	}
	desiredClientStatus := "follow_up_scheduled"
	if stage == "initial_contact" {
		desiredClientStatus = "initial_call_scheduled"
	}
	effectiveLeadID = foundLeadID

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, "", nil, fmt.Errorf("tx begin failed: %w", err)
	}
	defer tx.Rollback()

	if existingClientID != nil {
		clientID = *existingClientID
		if in.MergeStrategy != nil && *in.MergeStrategy == "overwrite" {
			if _, err := tx.ExecContext(ctx, `
				UPDATE clients SET
					name   = COALESCE(NULLIF($1,''), name),
					phone  = COALESCE(NULLIF($2,''), phone),
					source = COALESCE(NULLIF($3,''), source)
				WHERE id = $4`, in.Name, in.Phone, in.Source, clientID); err != nil {
				return 0, 0, "", nil, fmt.Errorf("client overwrite failed: %w", err)
			}
		}
	} else {
		insertErr := tx.QueryRowContext(ctx, `
			INSERT INTO clients (name, email, phone, source, source_stage_id, status)
			VALUES ($1,$2,$3,$4,$5,$6)
			ON CONFLICT (email) DO NOTHING
			RETURNING id`,
			in.Name, in.Email, in.Phone, in.Source, in.SourceStageID, desiredClientStatus,
		).Scan(&clientID)
		if insertErr == sql.ErrNoRows && strings.TrimSpace(in.Email) != "" {
			if err := tx.QueryRowContext(ctx,
				`SELECT id FROM clients WHERE LOWER(email) = LOWER($1) LIMIT 1`, in.Email,
			).Scan(&clientID); err != nil {
				return 0, 0, "", nil, fmt.Errorf("client dedup lookup failed: %w", err)
			}
		} else if insertErr != nil {
			return 0, 0, "", nil, fmt.Errorf("client insert failed: %w", insertErr)
		}

		if foundLeadID != nil {
			if _, err := tx.ExecContext(ctx,
				`UPDATE leads SET converted_client_id = $1 WHERE id = $2`, clientID, *foundLeadID); err != nil {
				return 0, 0, "", nil, fmt.Errorf("lead link failed: %w", err)
			}
		} else {
			var newLeadID int
			leadInsertErr := tx.QueryRowContext(ctx, `
				INSERT INTO leads (name, email, phone, source, source_stage_id, converted, converted_client_id)
				VALUES ($1,$2,$3,$4,$5,FALSE,$6)
				ON CONFLICT (email) DO NOTHING
				RETURNING id`,
				in.Name, in.Email, in.Phone, in.Source, in.SourceStageID, clientID,
			).Scan(&newLeadID)
			if leadInsertErr == nil {
				effectiveLeadID = &newLeadID
			} else if leadInsertErr == sql.ErrNoRows && strings.TrimSpace(in.Email) != "" {
				_ = tx.QueryRowContext(ctx,
					`SELECT id FROM leads WHERE LOWER(email) = LOWER($1) ORDER BY id DESC LIMIT 1`, in.Email,
				).Scan(&newLeadID)
				if newLeadID > 0 {
					effectiveLeadID = &newLeadID
				}
			}
		}
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE clients SET status = $1 WHERE id = $2 AND status IS NULL`,
		desiredClientStatus, clientID); err != nil {
		return 0, 0, "", nil, fmt.Errorf("client status backfill failed: %w", err)
	}

	if in.MergeStrategy != nil && *in.MergeStrategy == "overwrite" && foundLeadID != nil {
		if _, err := tx.ExecContext(ctx, `
			UPDATE leads SET
				name   = COALESCE(NULLIF($1,''), name),
				email  = COALESCE(NULLIF($2,''), email),
				phone  = COALESCE(NULLIF($3,''), phone),
				source = COALESCE(NULLIF($4,''), source),
				source_stage_id = COALESCE($5, source_stage_id)
			WHERE id = $6 AND converted = FALSE`,
			in.Name, in.Email, in.Phone, in.Source, in.SourceStageID, *foundLeadID); err != nil {
			return 0, 0, "", nil, fmt.Errorf("lead overwrite failed: %w", err)
		}
	}

	err = tx.QueryRowContext(ctx, `
		INSERT INTO sales_process (client_id, initial_contact_date, follow_up_date, stage, stage_id, created_at, lead_id)
		VALUES ($1,$2,$3,$4,$5,now(),$6)
		ON CONFLICT (client_id) DO NOTHING
		RETURNING id`,
		clientID, in.InitialContactDate, in.FollowUpDate, stage, in.SourceStageID, effectiveLeadID,
	).Scan(&salesID)
	if err == sql.ErrNoRows {
		if err := tx.QueryRowContext(ctx,
			`SELECT id FROM sales_process WHERE client_id = $1`, clientID,
		).Scan(&salesID); err != nil {
			return 0, 0, "", nil, fmt.Errorf("sales_process reuse failed: %w", err)
		}
		if in.SourceStageID != nil {
			if _, err := tx.ExecContext(ctx,
				`UPDATE sales_process SET stage_id = $1 WHERE id = $2 AND stage_id IS NULL`,
				in.SourceStageID, salesID); err != nil {
				return 0, 0, "", nil, fmt.Errorf("sales_process stage_id backfill failed: %w", err)
			}
		}
	} else if err != nil {
		return 0, 0, "", nil, fmt.Errorf("sales_process insert failed: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, "", nil, fmt.Errorf("commit failed: %w", err)
	}
	return clientID, salesID, stage, effectiveLeadID, nil
}

func (s *PostgresStore) ResolveLeadForSalesStart(ctx context.Context, leadID *int, email string) (foundLeadID *int, source string, stageID *int, err error) {
	if leadID != nil {
		var converted bool
		if err := s.db.QueryRowContext(ctx,
			`SELECT converted FROM leads WHERE id = $1`, *leadID,
		).Scan(&converted); err == nil && !converted {
			foundLeadID = leadID
		}
	}
	if foundLeadID == nil && strings.TrimSpace(email) != "" {
		var id int
		if err := s.db.QueryRowContext(ctx, `
			SELECT id FROM leads
			WHERE LOWER(email) = LOWER($1) AND converted = FALSE
			ORDER BY id DESC LIMIT 1`, email,
		).Scan(&id); err == nil {
			foundLeadID = &id
		}
	}
	if foundLeadID != nil {
		var leadSource sql.NullString
		var leadStageID sql.NullInt64
		if err := s.db.QueryRowContext(ctx,
			`SELECT source, source_stage_id FROM leads WHERE id = $1`, *foundLeadID,
		).Scan(&leadSource, &leadStageID); err == nil {
			if leadSource.Valid {
				source = leadSource.String
			}
			if leadStageID.Valid {
				v := int(leadStageID.Int64)
				stageID = &v
			}
		}
	}
	return foundLeadID, source, stageID, nil
}

func (s *PostgresStore) GetExistingClientBasic(ctx context.Context, clientID int) (domain.ClientBasic, error) {
	var cb domain.ClientBasic
	var phone, src sql.NullString
	var stageID sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, email, phone, source, source_stage_id FROM clients WHERE id = $1`, clientID,
	).Scan(&cb.ID, &cb.Name, &cb.Email, &phone, &src, &stageID)
	if err != nil {
		return domain.ClientBasic{}, err
	}
	if phone.Valid {
		cb.Phone = phone.String
	}
	if src.Valid {
		cb.Source = src.String
	}
	if stageID.Valid {
		v := int(stageID.Int64)
		cb.SourceStageID = &v
	}
	return cb, nil
}

func (s *PostgresStore) GetStartSalesResponseData(ctx context.Context, salesID, clientID int) ([]domain.Comment, domain.ClientBasic, error) {
	var comments []domain.Comment
	cmtRows, err := s.db.QueryContext(ctx, `
		SELECT id, client_id, entity_type, entity_id, author, body, metadata, created_at, updated_at
		FROM comments WHERE client_id = $1 ORDER BY created_at DESC`, clientID)
	if err == nil {
		defer cmtRows.Close()
		for cmtRows.Next() {
			var c domain.Comment
			var cid sql.NullInt64
			var author, metadata sql.NullString
			if err := cmtRows.Scan(&c.ID, &cid, &c.EntityType, &c.EntityID, &author, &c.Body, &metadata, &c.CreatedAt, &c.UpdatedAt); err != nil {
				continue
			}
			c.Author = nullStringToPtr(author)
			if cid.Valid {
				v := int(cid.Int64)
				c.ClientID = &v
			}
			if metadata.Valid && metadata.String != "" {
				_ = json.Unmarshal([]byte(metadata.String), &c.Metadata)
			}
			comments = append(comments, c)
		}
	}
	if comments == nil {
		comments = []domain.Comment{}
	}

	cb, err := s.GetExistingClientBasic(ctx, clientID)
	if err != nil {
		return nil, domain.ClientBasic{}, err
	}
	return comments, cb, nil
}

func (s *PostgresStore) EnsureContractForClosedSales(ctx context.Context, salesProcessID, clientID int, in EnsureContractInput) (*domain.ContractNotifyData, error) {
	if in.Closed == nil || !*in.Closed ||
		in.Revenue == nil ||
		in.ContractDurationMonths == nil || *in.ContractDurationMonths <= 0 ||
		in.ContractStartDate == nil || in.ContractFrequency == nil {
		return nil, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	sd, err := time.Parse("2006-01-02", *in.ContractStartDate)
	if err != nil {
		return nil, fmt.Errorf("invalid contract_start_date")
	}

	var existingContractID int
	err = tx.QueryRowContext(ctx,
		`SELECT id FROM contracts WHERE sales_process_id = $1 LIMIT 1`, salesProcessID,
	).Scan(&existingContractID)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	var notifyData *domain.ContractNotifyData

	if err == sql.ErrNoRows {
		spID := salesProcessID
		contractID, _, err := s.createContractTx(ctx, tx, ContractCreateInput{
			ClientID:         clientID,
			SalesProcessID:   &spID,
			StartDate:        sd,
			DurationMonths:   *in.ContractDurationMonths,
			RevenueTotal:     *in.Revenue,
			PaymentFreq:      *in.ContractFrequency,
			GenerateSchedule: true,
		})
		if err != nil {
			return nil, err
		}
		notifyData = &domain.ContractNotifyData{}
		notifyData.ClosureDate = sd.Format("2006-01-02")
		_ = contractID // will be used for notification by caller
	}

	if _, err = tx.ExecContext(ctx, `
		UPDATE leads SET converted = TRUE, converted_at = now(), converted_client_id = $1
		WHERE id = (SELECT lead_id FROM sales_process WHERE id = $2 AND lead_id IS NOT NULL)
		  AND converted = FALSE`, clientID, salesProcessID); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return notifyData, nil
}

// ErrConflictUpsellExists is returned by CreateOrUpdateUpsell when a blocking upsell already exists.
var ErrConflictUpsellExists = fmt.Errorf("cannot create upsell: edit the existing upsell instead")

// ErrInvalidContractStartDate is returned by EnsureContractForClosedSales on bad date.
var ErrInvalidContractStartDate = fmt.Errorf("invalid contract_start_date")

func (s *PostgresStore) ListClients(ctx context.Context, includeInactive bool) ([]domain.ClientRow, error) {
	rows, err := s.db.QueryContext(ctx, `
WITH client_status AS (
  SELECT
    c.id,
    (SELECT l.id FROM leads l WHERE l.converted_client_id = c.id
     ORDER BY l.converted_at DESC NULLS LAST, l.id DESC LIMIT 1) AS lead_id,
    c.name, c.email, c.phone, c.source,
    COALESCE(s.name, '') AS source_stage_name,
    CASE
      WHEN c.status = 'inactive' THEN 'inactive'
      WHEN c.status = 'lost' THEN 'lost'
      WHEN EXISTS (SELECT 1 FROM contracts ct WHERE ct.client_id = c.id AND (ct.end_date IS NULL OR ct.end_date >= CURRENT_DATE)) THEN 'active'
      WHEN EXISTS (SELECT 1 FROM contracts ct WHERE ct.client_id = c.id) THEN 'inactive'
      WHEN c.status IS NOT NULL AND c.status <> 'active' THEN c.status
      ELSE
        CASE
          WHEN sp.stage = 'closed' AND COALESCE(sp.closed, FALSE) = TRUE THEN 'inactive'
          WHEN sp.stage = 'lost' THEN 'lost'
          WHEN sp.stage = 'initial_contact' AND sp.initial_contact_date IS NOT NULL AND sp.follow_up_result IS NULL THEN 'initial_call_scheduled'
          WHEN sp.stage = 'follow_up' AND sp.follow_up_result IS NULL THEN 'follow_up_scheduled'
          WHEN sp.stage = 'follow_up' AND sp.follow_up_result IS TRUE THEN 'awaiting_response'
          ELSE 'inactive'
        END
    END AS status,
    c.completed_at
  FROM clients c
  LEFT JOIN stages s ON s.id = c.source_stage_id
  LEFT JOIN sales_process sp ON sp.client_id = c.id
)
SELECT * FROM client_status
WHERE ($1 = (status IN ('inactive', 'lost')))
ORDER BY id`, includeInactive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.ClientRow
	var clientIDs []int64
	idToIndex := make(map[int64]int)

	for rows.Next() {
		var cr domain.ClientRow
		var leadID sql.NullInt64
		var email, phone, source sql.NullString
		var completedAt sql.NullTime
		if err := rows.Scan(
			&cr.ID, &leadID, &cr.Name, &email, &phone, &source,
			&cr.SourceStageName, &cr.Status, &completedAt,
		); err != nil {
			return nil, err
		}
		if leadID.Valid {
			cr.LeadID = &leadID.Int64
		}
		if email.Valid {
			cr.Email = email.String
		}
		if phone.Valid {
			cr.Phone = phone.String
		}
		if source.Valid {
			cr.Source = source.String
		}
		if completedAt.Valid {
			v := completedAt.Time.Format("2006-01-02")
			cr.CompletedAt = &v
		}
		cr.Comments = []domain.Comment{}
		idToIndex[cr.ID] = len(out)
		clientIDs = append(clientIDs, cr.ID)
		out = append(out, cr)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(clientIDs) > 0 {
		cmtRows, err := s.db.QueryContext(ctx, `
			SELECT id, client_id, author, body, metadata, created_at, updated_at
			FROM comments WHERE client_id = ANY($1) ORDER BY created_at DESC`,
			pq.Array(clientIDs))
		if err == nil {
			defer cmtRows.Close()
			for cmtRows.Next() {
				var c domain.Comment
				var clientID int64
				var author, metadata sql.NullString
				if err := cmtRows.Scan(&c.ID, &clientID, &author, &c.Body, &metadata, &c.CreatedAt, &c.UpdatedAt); err != nil {
					continue
				}
				c.Author = nullStringToPtr(author)
				if metadata.Valid && metadata.String != "" {
					_ = json.Unmarshal([]byte(metadata.String), &c.Metadata)
				}
				if idx, ok := idToIndex[clientID]; ok {
					out[idx].Comments = append(out[idx].Comments, c)
				}
			}
		}
	}

	if out == nil {
		out = []domain.ClientRow{}
	}
	return out, nil
}
