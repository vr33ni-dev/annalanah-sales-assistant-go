package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/domain"
)

func scanUpsell(scanner interface {
	Scan(dest ...any) error
}, u *domain.ContractUpsell) error {
	var (
		upsellDate, createdAt, updatedAt   sql.NullTime
		upsellResult, contractFrequency    sql.NullString
		upsellRevenue                      sql.NullFloat64
		previousContractID, newContractID  sql.NullInt64
		contractStartDate                  sql.NullTime
		contractDurationMonths             sql.NullInt64
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

var ErrConflictUpsellExists = fmt.Errorf("cannot create upsell: edit the existing upsell instead")
