package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/domain"
)

func (s *PostgresStore) GetContractsInRange(ctx context.Context, typ string, start, end *time.Time) ([]domain.ContractSummary, error) {
	var sqlQuery string
	if typ == "neukunden" {
		sqlQuery = `
			SELECT c.id, c.client_id, cl.name, c.start_date, c.end_date, c.revenue_total
			FROM contracts c
			JOIN clients cl ON cl.id = c.client_id
			WHERE cl.status = 'active'
			  AND ($1::date IS NULL OR c.start_date >= $1::date)
			  AND ($2::date IS NULL OR c.start_date <= $2::date)
			  AND NOT EXISTS (
				SELECT 1 FROM contract_upsells cu
				WHERE cu.new_contract_id = c.id AND cu.upsell_result = 'verlaengerung'
			  )
			ORDER BY c.start_date DESC, c.id DESC
		`
	} else {
		sqlQuery = `
			SELECT c.id, c.client_id, cl.name, c.start_date, c.end_date,
				   COALESCE(cu.upsell_revenue, c.revenue_total) AS revenue
			FROM contracts c
			JOIN clients cl ON cl.id = c.client_id
			JOIN contract_upsells cu ON cu.new_contract_id = c.id AND cu.upsell_result = 'verlaengerung'
			WHERE cl.status = 'active'
			  AND ($1::date IS NULL OR c.start_date >= $1::date)
			  AND ($2::date IS NULL OR c.start_date <= $2::date)
			ORDER BY c.start_date DESC, c.id DESC
		`
	}

	rows, err := s.db.QueryContext(ctx, sqlQuery, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.ContractSummary
	for rows.Next() {
		var cs domain.ContractSummary
		var endDate sql.NullString
		if err := rows.Scan(&cs.ContractID, &cs.ClientID, &cs.ClientName, &cs.StartDate, &endDate, &cs.RevenueBrutto); err != nil {
			return nil, err
		}
		if endDate.Valid {
			cs.EndDate = &endDate.String
		}
		out = append(out, cs)
	}
	return out, rows.Err()
}

func (s *PostgresStore) GetDashboardKPIs(ctx context.Context, start, end *time.Time) (domain.DashboardKPIsRaw, error) {
	var kpis domain.DashboardKPIsRaw

	// 1. Upsell aggregates
	var verlaengerungsquote sql.NullFloat64
	if err := s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(upsell_revenue) FILTER (WHERE upsell_result = 'verlaengerung'), 0),
			COUNT(*) FILTER (WHERE upsell_result = 'verlaengerung'),
			COUNT(*) FILTER (WHERE upsell_result = 'keine_verlaengerung'),
			ROUND(
				100.0 * COUNT(*) FILTER (WHERE upsell_result = 'verlaengerung')
				/ NULLIF(COUNT(*) FILTER (WHERE upsell_result IN ('verlaengerung','keine_verlaengerung')), 0),
				1
			)
		FROM contract_upsells cu
		WHERE ($1::date IS NULL OR cu.upsell_date >= $1::date)
		  AND ($2::date IS NULL OR cu.upsell_date <= $2::date)
	`, start, end).Scan(
		&kpis.RenewalRevenueBrutto,
		&kpis.VerlaengerungCount,
		&kpis.KeineVerlaengerungCount,
		&verlaengerungsquote,
	); err != nil {
		return kpis, err
	}
	if verlaengerungsquote.Valid {
		kpis.Verlaengerungsquote = &verlaengerungsquote.Float64
	}

	// 2. Total revenue (all contracts starting in range, active clients)
	if err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(c.revenue_total), 0)
		FROM contracts c
		JOIN clients cl ON cl.id = c.client_id
		WHERE cl.status = 'active'
		  AND ($1::date IS NULL OR c.start_date >= $1::date)
		  AND ($2::date IS NULL OR c.start_date <= $2::date)
	`, start, end).Scan(&kpis.TotalRevenueBrutto); err != nil {
		return kpis, err
	}

	// 3. CLV active clients (all contract periods for active clients, all time)
	if err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(c.revenue_total), 0)
		FROM contracts c
		JOIN clients cl ON cl.id = c.client_id
		WHERE cl.status = 'active'
	`).Scan(&kpis.CLVActiveClientsBrutto); err != nil {
		return kpis, err
	}

	// 4. New customer revenue (contracts in range that are NOT renewals)
	if err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(c.revenue_total), 0)
		FROM contracts c
		JOIN clients cl ON cl.id = c.client_id
		WHERE cl.status = 'active'
		  AND ($1::date IS NULL OR c.start_date >= $1::date)
		  AND ($2::date IS NULL OR c.start_date <= $2::date)
		  AND NOT EXISTS (
			SELECT 1 FROM contract_upsells cu
			WHERE cu.new_contract_id = c.id AND cu.upsell_result = 'verlaengerung'
		  )
	`, start, end).Scan(&kpis.NewCustomerRevenueBrutto); err != nil {
		return kpis, err
	}

	// 5. Gesamt CLV (all contracts, all clients, all time)
	if err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(revenue_total), 0) FROM contracts
	`).Scan(&kpis.GesamtCLVBrutto); err != nil {
		return kpis, err
	}

	// 6. Active contracts (terminal period, not yet ended)
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM(c.revenue_total), 0)
		FROM contracts c
		JOIN clients cl ON cl.id = c.client_id
		WHERE (c.end_date IS NULL OR c.end_date >= CURRENT_DATE)
		  AND cl.status = 'active'
		  AND NOT EXISTS (
			SELECT 1 FROM contract_upsells
			WHERE previous_contract_id = c.id AND upsell_result = 'verlaengerung'
		  )
	`).Scan(&kpis.ActiveContractsCount, &kpis.ActiveRevenueBrutto); err != nil {
		return kpis, err
	}

	// 7. Won new-customer count
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM sales_process sp
		JOIN clients cl ON cl.id = sp.client_id
		WHERE sp.closed = true
		  AND sp.follow_up_date IS NOT NULL
		  AND COALESCE(sp.is_imported_placeholder, false) = false
		  AND ($1::date IS NULL OR cl.completed_at::date >= $1::date)
		  AND ($2::date IS NULL OR cl.completed_at::date <= $2::date)
		  AND NOT EXISTS (
			SELECT 1 FROM contract_upsells cu
			WHERE cu.client_id = sp.client_id
			  AND cu.upsell_date IS NOT NULL
			  AND cu.upsell_date < sp.follow_up_date
		  )
	`, start, end).Scan(&kpis.WonNewCount); err != nil {
		return kpis, err
	}

	// 8. Decided new-customer count
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM sales_process sp
		JOIN clients cl ON cl.id = sp.client_id
		WHERE (sp.closed = true OR (sp.closed = false AND sp.follow_up_result = true))
		  AND sp.follow_up_date IS NOT NULL
		  AND COALESCE(sp.is_imported_placeholder, false) = false
		  AND ($1::date IS NULL OR (
			CASE WHEN sp.closed = true THEN cl.completed_at::date
			     ELSE COALESCE(sp.updated_at, sp.follow_up_date, sp.created_at)::date
			END
		  ) >= $1::date)
		  AND ($2::date IS NULL OR (
			CASE WHEN sp.closed = true THEN cl.completed_at::date
			     ELSE COALESCE(sp.updated_at, sp.follow_up_date, sp.created_at)::date
			END
		  ) <= $2::date)
		  AND NOT EXISTS (
			SELECT 1 FROM contract_upsells cu
			WHERE cu.client_id = sp.client_id
			  AND cu.upsell_date IS NOT NULL
			  AND cu.upsell_date < sp.follow_up_date
		  )
	`, start, end).Scan(&kpis.DecidedNewCount); err != nil {
		return kpis, err
	}

	return kpis, nil
}

func (s *PostgresStore) GetMonthlyKPIs(ctx context.Context, year int) ([]domain.MonthlyKPIRaw, error) {
	out := make([]domain.MonthlyKPIRaw, 12)
	for i := range out {
		out[i].Month = i + 1
	}

	// Revenue per month
	revenueRows, err := s.db.QueryContext(ctx, `
		SELECT
			EXTRACT(MONTH FROM c.start_date)::int AS month,
			COALESCE(SUM(c.revenue_total), 0)     AS revenue
		FROM contracts c
		JOIN clients cl ON cl.id = c.client_id
		WHERE EXTRACT(YEAR FROM c.start_date) = $1
		  AND cl.status = 'active'
		GROUP BY month ORDER BY month
	`, year)
	if err != nil {
		return nil, err
	}
	defer revenueRows.Close()
	for revenueRows.Next() {
		var m int
		var rev float64
		if err := revenueRows.Scan(&m, &rev); err != nil {
			return nil, err
		}
		if m >= 1 && m <= 12 {
			out[m-1].Revenue = rev
		}
	}
	if err := revenueRows.Err(); err != nil {
		return nil, err
	}

	// Won new-customer deals per month
	wonRows, err := s.db.QueryContext(ctx, `
		SELECT
			EXTRACT(MONTH FROM cl.completed_at)::int AS month,
			COUNT(*) AS won_count
		FROM sales_process sp
		JOIN clients cl ON cl.id = sp.client_id
		WHERE sp.closed = true
		  AND sp.follow_up_date IS NOT NULL
		  AND COALESCE(sp.is_imported_placeholder, false) = false
		  AND EXTRACT(YEAR FROM cl.completed_at) = $1
		  AND NOT EXISTS (
			SELECT 1 FROM contract_upsells cu
			WHERE cu.client_id = sp.client_id
			  AND cu.upsell_date IS NOT NULL
			  AND cu.upsell_date < sp.follow_up_date
		  )
		GROUP BY month ORDER BY month
	`, year)
	if err != nil {
		return nil, err
	}
	defer wonRows.Close()
	for wonRows.Next() {
		var m, cnt int
		if err := wonRows.Scan(&m, &cnt); err != nil {
			return nil, err
		}
		if m >= 1 && m <= 12 {
			out[m-1].Won = cnt
		}
	}
	if err := wonRows.Err(); err != nil {
		return nil, err
	}

	// Decided new-customer deals per month
	decidedRows, err := s.db.QueryContext(ctx, `
		SELECT
			EXTRACT(MONTH FROM
				CASE WHEN sp.closed = true
					THEN cl.completed_at::date
					ELSE COALESCE(sp.updated_at, sp.follow_up_date, sp.created_at)::date
				END
			)::int AS month,
			COUNT(*) AS decided_count
		FROM sales_process sp
		JOIN clients cl ON cl.id = sp.client_id
		WHERE (sp.closed = true OR (sp.closed = false AND sp.follow_up_result = true))
		  AND sp.follow_up_date IS NOT NULL
		  AND COALESCE(sp.is_imported_placeholder, false) = false
		  AND EXTRACT(YEAR FROM
			CASE WHEN sp.closed = true
				THEN cl.completed_at::date
				ELSE COALESCE(sp.updated_at, sp.follow_up_date, sp.created_at)::date
			END
		  ) = $1
		  AND NOT EXISTS (
			SELECT 1 FROM contract_upsells cu
			WHERE cu.client_id = sp.client_id
			  AND cu.upsell_date IS NOT NULL
			  AND cu.upsell_date < sp.follow_up_date
		  )
		GROUP BY month ORDER BY month
	`, year)
	if err != nil {
		return nil, err
	}
	defer decidedRows.Close()
	for decidedRows.Next() {
		var m, cnt int
		if err := decidedRows.Scan(&m, &cnt); err != nil {
			return nil, err
		}
		if m >= 1 && m <= 12 {
			out[m-1].Decided = cnt
		}
	}
	return out, decidedRows.Err()
}
