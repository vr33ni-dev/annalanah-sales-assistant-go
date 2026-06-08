package store

import (
	"context"
)

func (s *PostgresStore) ExportClientsRaw(ctx context.Context) ([][]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			CAST(id AS TEXT),
			COALESCE(name, ''),
			COALESCE(email, ''),
			COALESCE(phone, ''),
			COALESCE(source, ''),
			COALESCE((SELECT name FROM stages WHERE id = clients.source_stage_id), ''),
			COALESCE(CAST(source_stage_id AS TEXT), ''),
			COALESCE(status, ''),
			COALESCE(CAST(completed_at AS TEXT), ''),
			COALESCE(CAST(created_at AS TEXT), '')
		FROM clients ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out [][]string
	for rows.Next() {
		var id, name, email, phone, source, stageName, stageID, status, completedAt, createdAt string
		if err := rows.Scan(&id, &name, &email, &phone, &source, &stageName, &stageID, &status, &completedAt, &createdAt); err != nil {
			return nil, err
		}
		out = append(out, []string{id, name, email, phone, source, stageName, stageID, status, completedAt, createdAt})
	}
	return out, rows.Err()
}

func (s *PostgresStore) ExportContractsRaw(ctx context.Context) ([][]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			CAST(id AS TEXT),
			COALESCE(CAST(client_id AS TEXT), ''),
			COALESCE(CAST(sales_process_id AS TEXT), ''),
			COALESCE(CAST(start_date AS TEXT), ''),
			COALESCE(CAST(end_date AS TEXT), ''),
			COALESCE(CAST(duration_months AS TEXT), ''),
			COALESCE(CAST(ROUND(CAST(revenue_total AS NUMERIC), 2) AS TEXT), ''),
			COALESCE(payment_frequency, ''),
			COALESCE(source, ''),
			COALESCE(CAST(created_at AS TEXT), ''),
			COALESCE(CAST(updated_at AS TEXT), '')
		FROM contracts ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out [][]string
	for rows.Next() {
		var id, clientID, salesProcID, start, end, dur, rev, freq, src, cat, uat string
		if err := rows.Scan(&id, &clientID, &salesProcID, &start, &end, &dur, &rev, &freq, &src, &cat, &uat); err != nil {
			return nil, err
		}
		out = append(out, []string{id, clientID, salesProcID, start, end, dur, rev, freq, src, cat, uat})
	}
	return out, rows.Err()
}

func (s *PostgresStore) ExportCashflowEntriesRaw(ctx context.Context) ([][]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			CAST(id AS TEXT),
			COALESCE(CAST(contract_id AS TEXT), ''),
			COALESCE(CAST(due_date AS TEXT), ''),
			COALESCE(CAST(ROUND(CAST(amount AS NUMERIC), 2) AS TEXT), ''),
			COALESCE(status, ''),
			COALESCE(CAST(updated_at AS TEXT), '')
		FROM cashflow_entries ORDER BY contract_id, due_date, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out [][]string
	for rows.Next() {
		var id, contractID, dueDate, amount, status, updatedAt string
		if err := rows.Scan(&id, &contractID, &dueDate, &amount, &status, &updatedAt); err != nil {
			return nil, err
		}
		out = append(out, []string{id, contractID, dueDate, amount, status, updatedAt})
	}
	return out, rows.Err()
}

func (s *PostgresStore) ExportLegacyCashflow(ctx context.Context) (LegacyCashflowData, error) {
	var data LegacyCashflowData

	rows, err := s.db.QueryContext(ctx, `
		SELECT cl.id,
			COALESCE(cl.name, ''),
			COALESCE(cl.status, ''),
			COALESCE(CAST(MIN(ct.start_date) AS TEXT), ''),
			COALESCE(CAST(MAX(ct.end_date) AS TEXT), ''),
			COALESCE(SUM(ct.revenue_total), 0),
			COALESCE(cl.source, ''),
			COALESCE((SELECT name FROM stages s WHERE s.id = cl.source_stage_id), '')
		FROM clients cl JOIN contracts ct ON ct.client_id = cl.id
		GROUP BY cl.id, cl.name, cl.status, cl.source, cl.source_stage_id
		ORDER BY cl.id`)
	if err != nil {
		return data, err
	}
	defer rows.Close()

	for rows.Next() {
		var c LegacyCashflowClientRow
		if err := rows.Scan(&c.ID, &c.Name, &c.Status, &c.StartDate, &c.EndDate, &c.CLV, &c.Source, &c.SourceStageName); err != nil {
			return data, err
		}
		data.Clients = append(data.Clients, c)
	}
	if err := rows.Err(); err != nil {
		return data, err
	}

	cashRows, err := s.db.QueryContext(ctx, `
		SELECT ct.client_id, substr(CAST(ce.due_date AS TEXT), 1, 7) AS ym, SUM(ce.amount)
		FROM cashflow_entries ce JOIN contracts ct ON ct.id = ce.contract_id
		GROUP BY ct.client_id, substr(CAST(ce.due_date AS TEXT), 1, 7)`)
	if err != nil {
		return data, err
	}
	defer cashRows.Close()

	data.AmountByClientMonth = make(map[int]map[string]float64)
	for cashRows.Next() {
		var clientID int
		var ym string
		var amount float64
		if err := cashRows.Scan(&clientID, &ym, &amount); err != nil {
			return data, err
		}
		if data.AmountByClientMonth[clientID] == nil {
			data.AmountByClientMonth[clientID] = make(map[string]float64)
		}
		data.AmountByClientMonth[clientID][ym] = amount
	}

	upsellRows, err := s.db.QueryContext(ctx, `
		SELECT client_id, upsell_result FROM contract_upsells
		ORDER BY client_id, upsell_date`)
	if err != nil {
		return data, err
	}
	defer upsellRows.Close()

	data.UpsellsByClient = make(map[int]map[string]int)
	for upsellRows.Next() {
		var clientID int
		var result string
		if err := upsellRows.Scan(&clientID, &result); err != nil {
			return data, err
		}
		if data.UpsellsByClient[clientID] == nil {
			data.UpsellsByClient[clientID] = make(map[string]int)
		}
		data.UpsellsByClient[clientID][result]++
	}

	commentRows, err := s.db.QueryContext(ctx, `
		SELECT entity_id, body FROM comments
		WHERE entity_type = 'client' ORDER BY entity_id, created_at`)
	if err != nil {
		return data, err
	}
	defer commentRows.Close()

	data.CommentsByClient = make(map[int][]string)
	for commentRows.Next() {
		var clientID int
		var body string
		if err := commentRows.Scan(&clientID, &body); err != nil {
			return data, err
		}
		data.CommentsByClient[clientID] = append(data.CommentsByClient[clientID], body)
	}

	return data, nil
}

func (s *PostgresStore) ExportAggregatedCashflow(ctx context.Context) (AggregatedCashflowData, error) {
	var data AggregatedCashflowData

	rows, err := s.db.QueryContext(ctx, `
		SELECT cl.id,
			COALESCE(cl.name, ''),
			COALESCE(cl.email, ''),
			COALESCE(cl.phone, ''),
			COALESCE(cl.source, ''),
			COALESCE((SELECT name FROM stages s WHERE s.id = cl.source_stage_id), ''),
			COALESCE(cl.status, ''),
			COALESCE(CAST(MIN(ct.start_date) AS TEXT), ''),
			COALESCE(CAST(MAX(ct.end_date) AS TEXT), ''),
			COALESCE(SUM(ct.revenue_total), 0)
		FROM clients cl JOIN contracts ct ON ct.client_id = cl.id
		GROUP BY cl.id, cl.name, cl.email, cl.phone, cl.source, cl.source_stage_id, cl.status
		ORDER BY cl.id`)
	if err != nil {
		return data, err
	}
	defer rows.Close()

	for rows.Next() {
		var c AggregatedCashflowClientRow
		if err := rows.Scan(&c.ID, &c.Name, &c.Email, &c.Phone, &c.Source, &c.SourceStageName,
			&c.Status, &c.StartDate, &c.EndDate, &c.TotalRevenue); err != nil {
			return data, err
		}
		data.Clients = append(data.Clients, c)
	}
	if err := rows.Err(); err != nil {
		return data, err
	}

	cashRows, err := s.db.QueryContext(ctx, `
		SELECT ct.client_id, substr(CAST(ce.due_date AS TEXT), 1, 7) AS ym, SUM(ce.amount)
		FROM cashflow_entries ce JOIN contracts ct ON ct.id = ce.contract_id
		GROUP BY ct.client_id, substr(CAST(ce.due_date AS TEXT), 1, 7)`)
	if err != nil {
		return data, err
	}
	defer cashRows.Close()

	data.AmountByClientMonth = make(map[int]map[string]float64)
	for cashRows.Next() {
		var clientID int
		var ym string
		var amount float64
		if err := cashRows.Scan(&clientID, &ym, &amount); err != nil {
			return data, err
		}
		if data.AmountByClientMonth[clientID] == nil {
			data.AmountByClientMonth[clientID] = make(map[string]float64)
		}
		data.AmountByClientMonth[clientID][ym] = amount
	}

	return data, nil
}
