package store

import (
	"database/sql"
	"strconv"
	"strings"
	"time"

	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/domain"
)

func (s *PostgresStore) ListCashflowEntries(f CashflowEntryFilter) ([]domain.CashflowEntry, int, error) {
	var where []string
	var args []interface{}
	idx := 1

	if f.ContractID != "" {
		where = append(where, "ce.contract_id = $"+strconv.Itoa(idx))
		args = append(args, f.ContractID)
		idx++
	}
	if f.ClientID != "" {
		where = append(where, "c.client_id = $"+strconv.Itoa(idx))
		args = append(args, f.ClientID)
		idx++
	}
	if f.Status != "" {
		where = append(where, "ce.status = $"+strconv.Itoa(idx))
		args = append(args, f.Status)
		idx++
	}
	if f.StartDate != nil {
		where = append(where, "ce.due_date >= $"+strconv.Itoa(idx))
		args = append(args, *f.StartDate)
		idx++
	}
	if f.EndDate != nil {
		where = append(where, "ce.due_date <= $"+strconv.Itoa(idx))
		args = append(args, *f.EndDate)
		idx++
	}

	whereSQL := ""
	if len(where) > 0 {
		whereSQL = "WHERE " + strings.Join(where, " AND ")
	}

	// Always join contracts+clients so we can return client_name.
	// When filtering by client_id, the WHERE already references c.client_id so the join is required anyway.
	joinSQL := `INNER JOIN contracts c ON c.id = ce.contract_id
	INNER JOIN clients cl ON cl.id = c.client_id `

	var total int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM cashflow_entries ce "+joinSQL+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	sortDir := "ASC"
	if strings.ToLower(f.SortOrder) == "desc" {
		sortDir = "DESC"
	}
	offset := (f.Page - 1) * f.PerPage
	dataQuery := `SELECT ce.id, ce.contract_id, ce.due_date::timestamp, ce.amount::float8, ce.status, ce.updated_at, cl.name
	FROM cashflow_entries ce
	` + joinSQL + whereSQL + `
	ORDER BY ce.due_date ` + sortDir + `, ce.id ` + sortDir + `
	LIMIT $` + strconv.Itoa(idx) + ` OFFSET $` + strconv.Itoa(idx+1)
	args = append(args, f.PerPage, offset)

	rows, err := s.db.Query(dataQuery, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var entries []domain.CashflowEntry
	for rows.Next() {
		var e domain.CashflowEntry
		var due, updated sql.NullTime
		var clientName sql.NullString
		if err := rows.Scan(&e.ID, &e.ContractID, &due, &e.Amount, &e.Status, &updated, &clientName); err != nil {
			return nil, 0, err
		}
		e.DueDate = nullTimeToString(due, time.RFC3339)
		e.UpdatedAt = nullTimeToString(updated, time.RFC3339)
		if clientName.Valid {
			e.ContractLabel = clientName.String
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return entries, total, nil
}

func (s *PostgresStore) UpdateCashflowEntryStatus(id int, status string) error {
	result, err := s.db.Exec(
		`UPDATE cashflow_entries SET status = $1, updated_at = NOW() WHERE id = $2`,
		status, id,
	)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) CashflowYTDPaid(start, end time.Time) (float64, error) {
	var total float64
	err := s.db.QueryRow(`
		SELECT COALESCE(SUM(amount) FILTER (
			WHERE COALESCE(status,'') <> 'not paid'
			  AND due_date::date >= $1
			  AND due_date::date <= $2
		), 0)
		FROM cashflow_entries
	`, start, end).Scan(&total)
	return total, err
}

func (s *PostgresStore) CashflowNextMonthsConfirmed(start, end time.Time) ([]domain.MonthConfirmed, error) {
	rows, err := s.db.Query(`
WITH months AS (
  SELECT to_char(d::date, 'YYYY-MM') AS ym,
         date_trunc('month', d)::date                        AS month_start,
         (date_trunc('month', d) + interval '1 month')::date AS month_end
  FROM generate_series($1::date, $2::date - interval '1 month', interval '1 month') AS d
),
entries AS (
  SELECT cf.due_date::date AS due_date, cf.amount::numeric AS amount
  FROM cashflow_entries cf
  WHERE cf.amount > 0 AND cf.due_date >= $1::date AND cf.due_date < $2::date
)
SELECT m.ym AS month, COALESCE(SUM(e.amount), 0)::numeric AS confirmed
FROM months m LEFT JOIN entries e ON e.due_date >= m.month_start AND e.due_date < m.month_end
GROUP BY m.ym ORDER BY m.ym
`, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.MonthConfirmed
	for rows.Next() {
		var m domain.MonthConfirmed
		if err := rows.Scan(&m.Month, &m.Confirmed); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *PostgresStore) CashflowForecast(start, end time.Time, avgRevenuePerContract float64, contractID *int) ([]domain.CashflowForecastRow, error) {
	var cidArg interface{} = sql.NullInt64{}
	if contractID != nil {
		cidArg = *contractID
	}
	rows, err := s.db.Query(`
WITH months AS (
  SELECT to_char(d::date, 'YYYY-MM') AS ym,
         date_trunc('month', d)::date                        AS month_start,
         (date_trunc('month', d) + interval '1 month')::date AS month_end
  FROM generate_series($1::date, $2::date - interval '1 month', interval '1 month') AS d
),
entries AS (
  SELECT cf.contract_id, cf.due_date::date AS due_date, cf.amount::numeric AS amount
  FROM cashflow_entries cf
  WHERE cf.amount > 0
    AND cf.due_date >= $1::date AND cf.due_date < $2::date
    AND ($4::int IS NULL OR cf.contract_id = $4)
),
confirmed AS (
  SELECT m.ym, COALESCE(SUM(e.amount), 0)::numeric AS amt
  FROM months m LEFT JOIN entries e ON e.due_date >= m.month_start AND e.due_date < m.month_end
  GROUP BY m.ym
),
potential AS (
  SELECT to_char(sp.follow_up_date, 'YYYY-MM') AS ym,
         SUM(CASE
               WHEN c.id IS NOT NULL AND c.duration_months > 0 THEN (c.revenue_total / c.duration_months)::numeric
               WHEN sp.revenue IS NOT NULL AND sp.revenue > 0   THEN sp.revenue::numeric
               ELSE $3::numeric
             END) AS amt
  FROM sales_process sp
  LEFT JOIN contracts c ON c.sales_process_id = sp.id
  WHERE sp.stage = 'follow_up'
    AND COALESCE(sp.closed, false) = false
    AND (sp.follow_up_result IS NULL OR sp.follow_up_result = true)
    AND sp.follow_up_date IS NOT NULL
    AND sp.follow_up_date >= $1::date AND sp.follow_up_date < $2::date
    AND ($4::int IS NULL)
  GROUP BY 1
),
confirmed_collapsed AS (SELECT ym, SUM(amt)::numeric AS amt FROM confirmed GROUP BY ym),
joined AS (
  SELECT m.ym,
         COALESCE(cc.amt, 0) AS confirmed,
         COALESCE(pt.amt, 0) AS potential
  FROM months m
  LEFT JOIN confirmed_collapsed cc ON cc.ym = m.ym
  LEFT JOIN potential pt ON pt.ym = m.ym
)
SELECT ym AS month, confirmed, potential FROM joined ORDER BY month
`, start, end, avgRevenuePerContract, cidArg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.CashflowForecastRow
	for rows.Next() {
		var row domain.CashflowForecastRow
		if err := rows.Scan(&row.Month, &row.Confirmed, &row.Potential); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
