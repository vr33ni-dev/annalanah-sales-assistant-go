package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/domain"
)

// normalizePaymentFrequency validates and normalizes payment frequency.
func normalizePaymentFrequency(paymentFreq string, durationMonths int) (string, error) {
	pf := strings.ToLower(strings.TrimSpace(paymentFreq))
	switch pf {
	case "monthly", "bi-monthly", "quarterly", "one-time", "bi-yearly":
	default:
		return "", fmt.Errorf("invalid payment_frequency (allowed: monthly, bi-monthly, quarterly, one-time, bi-yearly)")
	}
	if pf == "bi-yearly" && durationMonths < 12 {
		return "", fmt.Errorf("bi-yearly payment frequency requires duration_months >= 12")
	}
	return pf, nil
}

func addMonthClamped(t time.Time, months int) time.Time {
	year, month, day := t.Date()
	loc := t.Location()
	target := time.Date(year, month+time.Month(months), 1, 0, 0, 0, 0, loc)
	lastDay := target.AddDate(0, 1, -1).Day()
	if day > lastDay {
		day = lastDay
	}
	return time.Date(target.Year(), target.Month(), day, 0, 0, 0, 0, loc)
}

func insertCashflowEntriesTx(tx *sql.Tx, contractID int, startDate, endDate time.Time, revenueTotal float64, paymentFreq string) error {
	if endDate.Before(startDate) {
		return fmt.Errorf("endDate cannot be before startDate")
	}
	step := 1
	switch paymentFreq {
	case "bi-monthly":
		step = 2
	case "quarterly":
		step = 3
	case "bi-yearly":
		step = 6
	case "one-time":
		step = 0
	}

	stmt := `INSERT INTO cashflow_entries (contract_id, due_date, amount, status)
		VALUES ($1, $2::date, $3, 'confirmed')
		ON CONFLICT (contract_id, due_date) DO NOTHING`
	ctx := context.Background()

	if paymentFreq == "one-time" {
		_, err := tx.ExecContext(ctx, stmt, contractID, startDate.Format("2006-01-02"), revenueTotal)
		return err
	}

	periods := 0
	cur := startDate
	for {
		next := addMonthClamped(cur, step)
		if next.After(endDate) {
			break
		}
		periods++
		cur = next
	}
	if periods == 0 {
		return nil
	}
	amt := revenueTotal / float64(periods)
	cur = startDate
	for i := 0; i < periods; i++ {
		if _, err := tx.ExecContext(ctx, stmt, contractID, cur.Format("2006-01-02"), amt); err != nil {
			return err
		}
		cur = addMonthClamped(cur, step)
	}
	return nil
}

func insertImportedCashflowEntriesTx(tx *sql.Tx, contractID, clientID int, cashflows map[string]interface{}) error {
	numRe := regexp.MustCompile(`[-+]?[0-9]*\.?[0-9]+`)
	for ym, value := range cashflows {
		date, err := time.Parse("2006-01", ym)
		if err != nil {
			continue
		}
		switch v := value.(type) {
		case float64:
			if v == 0 {
				continue
			}
			if v < 100 {
				v *= 1000
			}
			if _, err := tx.Exec(`INSERT INTO cashflow_entries (contract_id, due_date, amount, status)
				VALUES ($1, $2::date, $3, 'confirmed')`, contractID, date, v); err != nil {
				return err
			}
		case string:
			trimmed := strings.TrimSpace(v)
			if trimmed == "" || strings.ToLower(trimmed) == "-" {
				continue
			}
			numStr := numRe.FindString(trimmed)
			if numStr != "" {
				if n, err := strconv.ParseFloat(numStr, 64); err == nil && n != 0 {
					if n < 100 {
						n *= 1000
					}
					if _, err := tx.Exec(`INSERT INTO cashflow_entries (contract_id, due_date, amount, status)
						VALUES ($1, $2::date, $3, 'confirmed')`, contractID, date, n); err != nil {
						return err
					}
				}
				leftover := strings.TrimSpace(strings.Replace(trimmed, numStr, "", 1))
				if leftover != "" {
					body := fmt.Sprintf("%s: %s", ym, trimmed)
					if _, err := tx.Exec(`INSERT INTO comments (entity_type, entity_id, client_id, body, author)
						VALUES ('client', $1, $1, $2, 'importer')`, clientID, body); err != nil {
						return err
					}
				}
				continue
			}
			body := fmt.Sprintf("%s: %s", ym, trimmed)
			if _, err := tx.Exec(`INSERT INTO comments (entity_type, entity_id, client_id, body, author)
				VALUES ('client', $1, $1, $2, 'importer')`, clientID, body); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *PostgresStore) createContractTx(ctx context.Context, tx *sql.Tx, in ContractCreateInput) (int, *string, error) {
	pf, err := normalizePaymentFrequency(in.PaymentFreq, in.DurationMonths)
	if err != nil {
		return 0, nil, err
	}
	source := in.Source
	if source == "" {
		source = "manual"
	}
	ed := in.StartDate.AddDate(0, in.DurationMonths, 0)
	if in.EndDate != nil {
		if in.EndDate.Before(in.StartDate) {
			return 0, nil, fmt.Errorf("end_date cannot be before start_date")
		}
		ed = *in.EndDate
	}

	var contractID int
	var createdAt sql.NullTime

	if in.CreatedAtOverride != nil {
		err = tx.QueryRowContext(ctx, `
			INSERT INTO contracts (client_id, sales_process_id, start_date, end_date, duration_months, revenue_total, payment_frequency, source, created_at)
			VALUES ($1, $2, $3::date, $4::date, $5, $6, $7, $8, $9)
			ON CONFLICT (sales_process_id, start_date) WHERE sales_process_id IS NOT NULL DO NOTHING
			RETURNING id, created_at`,
			in.ClientID, in.SalesProcessID, in.StartDate, ed, in.DurationMonths, in.RevenueTotal, pf, source, *in.CreatedAtOverride,
		).Scan(&contractID, &createdAt)
	} else {
		err = tx.QueryRowContext(ctx, `
			INSERT INTO contracts (client_id, sales_process_id, start_date, end_date, duration_months, revenue_total, payment_frequency, source)
			VALUES ($1, $2, $3::date, $4::date, $5, $6, $7, $8)
			ON CONFLICT (sales_process_id, start_date) WHERE sales_process_id IS NOT NULL DO NOTHING
			RETURNING id, created_at`,
			in.ClientID, in.SalesProcessID, in.StartDate, ed, in.DurationMonths, in.RevenueTotal, pf, source,
		).Scan(&contractID, &createdAt)
	}

	if err == sql.ErrNoRows && in.SalesProcessID != nil {
		var existCat sql.NullTime
		err = tx.QueryRowContext(ctx,
			`SELECT id, created_at FROM contracts WHERE sales_process_id = $1 AND start_date = $2::date LIMIT 1`,
			in.SalesProcessID, in.StartDate,
		).Scan(&contractID, &existCat)
		createdAt = existCat
	}
	if err != nil {
		return 0, nil, err
	}

	if in.GenerateSchedule && in.DurationMonths > 0 {
		if err := insertCashflowEntriesTx(tx, contractID, in.StartDate, ed, in.RevenueTotal, pf); err != nil {
			return 0, nil, fmt.Errorf("failed to insert cashflow entries: %w", err)
		}
	}

	if createdAt.Valid {
		s := createdAt.Time.Format(time.RFC3339)
		return contractID, &s, nil
	}
	return contractID, nil, nil
}

func (s *PostgresStore) ListContracts(ctx context.Context, includeExpired, includeComments, includeCashflow bool) ([]domain.ContractRow, error) {
	query := `
WITH RECURSIVE upsell_chain(terminal_id, node_id) AS (
	SELECT c.id, c.id FROM contracts c
	WHERE c.id NOT IN (
		SELECT previous_contract_id FROM contract_upsells
		WHERE previous_contract_id IS NOT NULL AND upsell_result = 'verlaengerung'
	)
	UNION ALL
	SELECT uc.terminal_id, cu.previous_contract_id
	FROM upsell_chain uc
	JOIN contract_upsells cu ON cu.new_contract_id = uc.node_id
		AND cu.upsell_result = 'verlaengerung' AND cu.previous_contract_id IS NOT NULL
),
chain_stats AS (
	SELECT uc.terminal_id,
		SUM(c2.revenue_total) AS total_revenue,
		SUM(c2.duration_months) AS total_duration_months,
		MIN(c2.start_date) AS chain_start_date
	FROM upsell_chain uc JOIN contracts c2 ON c2.id = uc.node_id
	GROUP BY uc.terminal_id
),
overdue AS (
	SELECT contract_id, MIN(due_date)::date AS overdue_due_date
	FROM cashflow_entries WHERE status = 'overdue' GROUP BY contract_id
),
upcoming AS (
	SELECT contract_id, MIN(due_date)::date AS upcoming_due_date
	FROM cashflow_entries WHERE due_date >= CURRENT_DATE GROUP BY contract_id
)
SELECT c.id, c.client_id, cl.name, c.sales_process_id,
	cs.chain_start_date::text, c.end_date::text, c.created_at::text,
	cs.total_duration_months::int,
	ROUND(cs.total_revenue::numeric, 2),
	c.payment_frequency,
	CASE WHEN cs.total_duration_months > 0 THEN ROUND((cs.total_revenue / cs.total_duration_months)::numeric, 2) ELSE 0 END,
	COALESCE(o.overdue_due_date, u.upcoming_due_date)::text,
	c.source
FROM contracts c
JOIN clients cl ON cl.id = c.client_id
JOIN chain_stats cs ON cs.terminal_id = c.id
LEFT JOIN overdue o ON o.contract_id = c.id
LEFT JOIN upcoming u ON u.contract_id = c.id`

	if !includeExpired {
		query += `
WHERE (c.end_date IS NULL OR c.end_date >= CURRENT_DATE) AND cl.status = 'active'`
	} else {
		query += `
WHERE cl.status = 'inactive' OR c.end_date < CURRENT_DATE`
	}
	query += "\nORDER BY c.id;"

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.ContractRow
	var contractIDs []int
	idToIndex := make(map[int]int)

	for rows.Next() {
		var cr domain.ContractRow
		var endDate, createdAt, nextDueDate sql.NullString
		if err := rows.Scan(
			&cr.ID, &cr.ClientID, &cr.ClientName, &cr.SalesProcessID,
			&cr.StartDate, &endDate, &createdAt, &cr.DurationMonths, &cr.RevenueBrutto,
			&cr.PaymentFreq, &cr.BaseMonthlyBrutto, &nextDueDate, &cr.Source,
		); err != nil {
			return nil, err
		}
		if endDate.Valid {
			cr.EndDate = &endDate.String
		}
		if createdAt.Valid {
			cr.CreatedAt = &createdAt.String
		}
		if nextDueDate.Valid {
			cr.NextDueDate = &nextDueDate.String
		}
		cr.Comments = []domain.Comment{}
		cr.CashflowEntries = []domain.CashflowEntry{}
		idToIndex[cr.ID] = len(out)
		contractIDs = append(contractIDs, cr.ID)
		out = append(out, cr)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if includeComments && len(contractIDs) > 0 {
		cmtRows, err := s.db.QueryContext(ctx, `
			SELECT id, entity_id, author, body, metadata, created_at, updated_at
			FROM comments WHERE entity_type = 'contract' AND entity_id = ANY($1)
			ORDER BY created_at DESC`, pq.Array(contractIDs))
		if err == nil {
			defer cmtRows.Close()
			for cmtRows.Next() {
				var c domain.Comment
				var author, metadata sql.NullString
				if err := cmtRows.Scan(&c.ID, &c.EntityID, &author, &c.Body, &metadata, &c.CreatedAt, &c.UpdatedAt); err != nil {
					continue
				}
				c.EntityType = "contract"
				c.Author = nullStringToPtr(author)
				if metadata.Valid && metadata.String != "" {
					_ = json.Unmarshal([]byte(metadata.String), &c.Metadata)
				}
				if idx, ok := idToIndex[c.EntityID]; ok {
					out[idx].Comments = append(out[idx].Comments, c)
				}
			}
		}
	}

	if includeCashflow && len(contractIDs) > 0 {
		ceRows, err := s.db.QueryContext(ctx, `
			SELECT id, contract_id, due_date, amount, status, updated_at
			FROM cashflow_entries WHERE contract_id = ANY($1)
			ORDER BY contract_id, due_date, id`, pq.Array(contractIDs))
		if err == nil {
			defer ceRows.Close()
			for ceRows.Next() {
				var ce domain.CashflowEntry
				var dueDate, updatedAt sql.NullTime
				if err := ceRows.Scan(&ce.ID, &ce.ContractID, &dueDate, &ce.Amount, &ce.Status, &updatedAt); err != nil {
					continue
				}
				if dueDate.Valid {
					v := dueDate.Time.Format(time.RFC3339)
					ce.DueDate = &v
				}
				if updatedAt.Valid {
					v := updatedAt.Time.Format(time.RFC3339)
					ce.UpdatedAt = &v
				}
				if idx, ok := idToIndex[ce.ContractID]; ok {
					out[idx].CashflowEntries = append(out[idx].CashflowEntries, ce)
				}
			}
		}
	}

	if out == nil {
		out = []domain.ContractRow{}
	}
	return out, nil
}

func (s *PostgresStore) GetContractByID(ctx context.Context, id int) (domain.ContractRow, error) {
	var cr domain.ContractRow
	var endDate, endDateOverride, createdAt, updatedAt, nextDueDate sql.NullString
	err := s.db.QueryRowContext(ctx, `
WITH overdue AS (
	SELECT contract_id, MIN(due_date)::date AS overdue_due_date
	FROM cashflow_entries WHERE status = 'overdue' GROUP BY contract_id
),
upcoming AS (
	SELECT contract_id, MIN(due_date)::date AS upcoming_due_date
	FROM cashflow_entries WHERE due_date >= CURRENT_DATE GROUP BY contract_id
)
SELECT c.id, c.client_id, cl.name, c.sales_process_id,
c.start_date::text, c.end_date::text, c.end_date_override::text, c.created_at::text, c.updated_at::text, c.duration_months, ROUND(c.revenue_total::numeric, 2), c.payment_frequency,
	CASE WHEN c.duration_months > 0 THEN ROUND((c.revenue_total / c.duration_months)::numeric, 2) ELSE 0 END,
	COALESCE(o.overdue_due_date, u.upcoming_due_date)::text, c.source
FROM contracts c
JOIN clients cl ON cl.id = c.client_id
LEFT JOIN overdue o ON o.contract_id = c.id
LEFT JOIN upcoming u ON u.contract_id = c.id
WHERE c.id = $1`, id).Scan(
		&cr.ID, &cr.ClientID, &cr.ClientName, &cr.SalesProcessID,
		&cr.StartDate, &endDate, &endDateOverride, &createdAt, &updatedAt, &cr.DurationMonths, &cr.RevenueBrutto,
		&cr.PaymentFreq, &cr.BaseMonthlyBrutto, &nextDueDate, &cr.Source,
	)
	if err != nil {
		return domain.ContractRow{}, err
	}
	if endDate.Valid {
		cr.EndDate = &endDate.String
	}
	if endDateOverride.Valid {
		cr.EndDateOverride = &endDateOverride.String
	}
	if createdAt.Valid {
		cr.CreatedAt = &createdAt.String
	}
	if updatedAt.Valid {
		cr.UpdatedAt = &updatedAt.String
	}
	if nextDueDate.Valid {
		cr.NextDueDate = &nextDueDate.String
	}

	// Chain
	cr.Chain = []domain.ContractRow{}
	chainRows, err := s.db.QueryContext(ctx, `
WITH RECURSIVE
chain_root(contract_id) AS (
	SELECT $1::int
	UNION ALL
	SELECT cu.previous_contract_id FROM chain_root cr
	JOIN contract_upsells cu ON cu.new_contract_id = cr.contract_id
		AND cu.upsell_result = 'verlaengerung' AND cu.previous_contract_id IS NOT NULL
),
root AS (SELECT contract_id FROM chain_root ORDER BY contract_id ASC LIMIT 1),
chain_forward(contract_id) AS (
	SELECT r.contract_id FROM root r
	UNION ALL
	SELECT cu.new_contract_id FROM chain_forward cf
	JOIN contract_upsells cu ON cu.previous_contract_id = cf.contract_id
		AND cu.upsell_result = 'verlaengerung' AND cu.new_contract_id IS NOT NULL
)
SELECT c.id, c.client_id, cl.name, c.sales_process_id,
	c.start_date::text, c.end_date::text, c.end_date_override::text, c.created_at::text, c.duration_months,
	ROUND(c.revenue_total::numeric, 2), c.payment_frequency,
	CASE WHEN c.duration_months > 0 THEN ROUND((c.revenue_total / c.duration_months)::numeric, 2) ELSE 0 END,
	COALESCE(o.overdue_due_date, u.upcoming_due_date)::text, c.source
FROM chain_forward cf
JOIN contracts c ON c.id = cf.contract_id
JOIN clients cl ON cl.id = c.client_id
LEFT JOIN (SELECT contract_id, MIN(due_date)::date AS overdue_due_date FROM cashflow_entries WHERE status = 'overdue' GROUP BY contract_id) o ON o.contract_id = c.id
LEFT JOIN (SELECT contract_id, MIN(due_date)::date AS upcoming_due_date FROM cashflow_entries WHERE due_date >= CURRENT_DATE GROUP BY contract_id) u ON u.contract_id = c.id
ORDER BY c.start_date ASC, c.id ASC`, id)
	if err == nil {
		defer chainRows.Close()
		for chainRows.Next() {
			var cx domain.ContractRow
			var cxEnd, cxEndDateOverride, cxCreated, cxNext sql.NullString
			if err := chainRows.Scan(
				&cx.ID, &cx.ClientID, &cx.ClientName, &cx.SalesProcessID,
				&cx.StartDate, &cxEnd, &cxEndDateOverride, &cxCreated, &cx.DurationMonths,
				&cx.RevenueBrutto, &cx.PaymentFreq, &cx.BaseMonthlyBrutto, &cxNext, &cx.Source,
			); err == nil {
				if cxEnd.Valid {
					cx.EndDate = &cxEnd.String
				}
				if cxEndDateOverride.Valid {
					cx.EndDateOverride = &cxEndDateOverride.String
				}
				if cxCreated.Valid {
					cx.CreatedAt = &cxCreated.String
				}
				if cxNext.Valid {
					cx.NextDueDate = &cxNext.String
				}
				cr.Chain = append(cr.Chain, cx)
			}
		}
	}

	// Comments
	cr.Comments = []domain.Comment{}
	cmtRows, err := s.db.QueryContext(ctx, `
		SELECT id, entity_id, author, body, metadata, created_at, updated_at
		FROM comments WHERE entity_type = 'contract' AND entity_id = $1
		ORDER BY created_at DESC`, id)
	if err == nil {
		defer cmtRows.Close()
		for cmtRows.Next() {
			var c domain.Comment
			var author, metadata sql.NullString
			if err := cmtRows.Scan(&c.ID, &c.EntityID, &author, &c.Body, &metadata, &c.CreatedAt, &c.UpdatedAt); err != nil {
				continue
			}
			c.EntityType = "contract"
			c.Author = nullStringToPtr(author)
			if metadata.Valid && metadata.String != "" {
				_ = json.Unmarshal([]byte(metadata.String), &c.Metadata)
			}
			cr.Comments = append(cr.Comments, c)
		}
	}

	// Cashflow
	cr.CashflowEntries = []domain.CashflowEntry{}
	ceRows, err := s.db.QueryContext(ctx, `
		SELECT id, contract_id, due_date, amount, status, updated_at
		FROM cashflow_entries WHERE contract_id = $1
		ORDER BY due_date, id`, id)
	if err == nil {
		defer ceRows.Close()
		for ceRows.Next() {
			var ce domain.CashflowEntry
			var dueDate, updatedAt sql.NullTime
			if err := ceRows.Scan(&ce.ID, &ce.ContractID, &dueDate, &ce.Amount, &ce.Status, &updatedAt); err != nil {
				continue
			}
			if dueDate.Valid {
				v := dueDate.Time.Format(time.RFC3339)
				ce.DueDate = &v
			}
			if updatedAt.Valid {
				v := updatedAt.Time.Format(time.RFC3339)
				ce.UpdatedAt = &v
			}
			cr.CashflowEntries = append(cr.CashflowEntries, ce)
		}
	}

	return cr, nil
}

func (s *PostgresStore) GetContractCashflow(ctx context.Context, contractID int) ([]domain.CashflowEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, contract_id, due_date, amount, status, updated_at
		FROM cashflow_entries WHERE contract_id = $1
		ORDER BY due_date`, contractID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.CashflowEntry
	for rows.Next() {
		var ce domain.CashflowEntry
		var dueDate, updatedAt sql.NullTime
		if err := rows.Scan(&ce.ID, &ce.ContractID, &dueDate, &ce.Amount, &ce.Status, &updatedAt); err != nil {
			return nil, err
		}
		if dueDate.Valid {
			v := dueDate.Time.Format(time.RFC3339)
			ce.DueDate = &v
		}
		if updatedAt.Valid {
			v := updatedAt.Time.Format(time.RFC3339)
			ce.UpdatedAt = &v
		}
		out = append(out, ce)
	}
	if out == nil {
		out = []domain.CashflowEntry{}
	}
	return out, rows.Err()
}

func (s *PostgresStore) CreateContract(ctx context.Context, in ContractCreateInput) (contractID int, createdAt *string, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, nil, err
	}
	defer tx.Rollback()
	id, cat, err := s.createContractTx(ctx, tx, in)
	if err != nil {
		return 0, nil, err
	}
	if err := tx.Commit(); err != nil {
		return 0, nil, err
	}
	return id, cat, nil
}

func (s *PostgresStore) UpdateContract(ctx context.Context, id int, sd, ed time.Time, durationMonths int, revenueTotal float64, paymentFreq string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		UPDATE contracts
		SET start_date = $1, end_date = $2, duration_months = $3,
		    revenue_total = $4, payment_frequency = $5, updated_at = NOW()
		WHERE id = $6`, sd, ed, durationMonths, revenueTotal, paymentFreq, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM cashflow_entries WHERE contract_id = $1`, id); err != nil {
		return err
	}
	if err := insertCashflowEntriesTx(tx, id, sd, ed, revenueTotal, paymentFreq); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *PostgresStore) GetContractClientID(ctx context.Context, contractID int) (int, error) {
	var clientID int
	err := s.db.QueryRowContext(ctx, `SELECT client_id FROM contracts WHERE id = $1`, contractID).Scan(&clientID)
	return clientID, err
}

func (s *PostgresStore) GetContractNotifyData(ctx context.Context, contractID int) (domain.ContractNotifyData, error) {
	var data domain.ContractNotifyData
	var clientName, closureDate, source, sourceStageName, salesStageName, nextDueDate sql.NullString

	err := s.db.QueryRowContext(ctx, `
		SELECT
			cl.name,
			COALESCE(cl.completed_at::date, sp.follow_up_date::date)::text AS closure_date,
			cl.source,
			src_st.name AS source_stage_name,
			COALESCE(st.name, sp.stage) AS sales_stage_name,
			(SELECT MIN(due_date)::date::text FROM cashflow_entries
			 WHERE contract_id = c.id AND status <> 'paid') AS next_due_date
		FROM contracts c
		JOIN clients cl ON cl.id = c.client_id
		LEFT JOIN sales_process sp ON sp.id = c.sales_process_id
		LEFT JOIN stages st ON st.id = sp.stage_id
		LEFT JOIN stages src_st ON src_st.id = cl.source_stage_id
		WHERE c.id = $1`, contractID,
	).Scan(&clientName, &closureDate, &source, &sourceStageName, &salesStageName, &nextDueDate)
	if err != nil {
		return data, err
	}

	if clientName.Valid {
		data.ClientName = clientName.String
	}
	if closureDate.Valid {
		data.ClosureDate = closureDate.String
	}
	if source.Valid {
		data.Source = strings.TrimSpace(source.String)
	}
	if sourceStageName.Valid && strings.TrimSpace(sourceStageName.String) != "" {
		data.StageName = strings.TrimSpace(sourceStageName.String)
	} else if salesStageName.Valid {
		data.StageName = strings.TrimSpace(salesStageName.String)
	}
	if nextDueDate.Valid {
		data.NextDueDate = nextDueDate.String
	}
	return data, nil
}

func (s *PostgresStore) PauseContract(ctx context.Context, contractID int, newEndDate string, reason string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Fetch start_date and current end_date
	var startDate time.Time
	var oldEndDate sql.NullTime
	if err := tx.QueryRowContext(ctx,
		`SELECT start_date, end_date FROM contracts WHERE id = $1`, contractID,
	).Scan(&startDate, &oldEndDate); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("contract not found")
		}
		return err
	}

	newEnd, _ := time.Parse("2006-01-02", newEndDate) // already validated in handler
	if newEnd.Before(startDate) {
		return fmt.Errorf("new_end_date cannot be before start_date")
	}

	var deltaDays int
	if oldEndDate.Valid {
		deltaDays = int(newEnd.Sub(oldEndDate.Time).Hours() / 24)
	}

	// Update end_date and end_date_override
	if _, err := tx.ExecContext(ctx, `
        UPDATE contracts
        SET end_date          = $1::date,
            end_date_override = $1::date,
            updated_at        = NOW()
        WHERE id = $2`, newEndDate, contractID); err != nil {
		return err
	}

	// Shift all non-paid cashflow entries by delta
	if deltaDays != 0 {
		if _, err := tx.ExecContext(ctx, `
            UPDATE cashflow_entries
            SET due_date   = due_date + ($1::int * INTERVAL '1 day'),
                updated_at = NOW()
            WHERE contract_id = $2
              AND status IN ('confirmed', 'overdue')`,
			deltaDays, contractID); err != nil {
			return err
		}
	}

	// Store reason as a comment linked to this contract
	if _, err := tx.ExecContext(ctx, `
        INSERT INTO comments (entity_type, entity_id, client_id, author, body)
        SELECT 'contract', $1, client_id, 'system', $2
        FROM contracts WHERE id = $1`,
		contractID, reason); err != nil {
		return err
	}

	return tx.Commit()
}
