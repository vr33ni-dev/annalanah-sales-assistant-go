package store

import (
	"database/sql"
	"encoding/json"

	"github.com/lib/pq"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/domain"
)

func (s *PostgresStore) GetSalesProcess(id int) (domain.SalesProcess, error) {
	row := s.db.QueryRow(`
	  SELECT
	    sp.id, sp.client_id,
	    c.name, c.email, c.phone, c.source, c.completed_at,
	    sp.stage, sp.initial_contact_date, sp.follow_up_date, sp.follow_up_result,
	    sp.closed,
	    CASE WHEN COALESCE(sp.closed, false) THEN sp.revenue ELSE NULL END,
	    sp.stage_id
	  FROM sales_process sp
	  JOIN clients c ON c.id = sp.client_id
	  WHERE sp.id = $1
	`, id)

	var sp domain.SalesProcess
	var completedAt sql.NullTime
	var email, phone, source sql.NullString
	if err := row.Scan(
		&sp.ID, &sp.ClientID,
		&sp.ClientName, &email, &phone, &source, &completedAt,
		&sp.Stage, &sp.InitialContactDate, &sp.FollowUpDate, &sp.FollowUpResult,
		&sp.Closed, &sp.Revenue, &sp.StageID,
	); err != nil {
		return domain.SalesProcess{}, err
	}
	sp.ClientEmail = nullStringToPtr(email)
	sp.ClientPhone = nullStringToPtr(phone)
	sp.ClientSource = nullStringToPtr(source)
	sp.CompletedAt = nullTimeToString(completedAt, "2006-01-02")

	sp.Comments = []domain.Comment{}
	commentRows, err := s.db.Query(`
		SELECT id, client_id, entity_type, entity_id, author, body, metadata, created_at, updated_at
		FROM comments WHERE client_id = $1 ORDER BY created_at DESC
	`, sp.ClientID)
	if err == nil {
		defer commentRows.Close()
		for commentRows.Next() {
			var c domain.Comment
			var cid sql.NullInt64
			var author, metadata sql.NullString
			if err := commentRows.Scan(&c.ID, &cid, &c.EntityType, &c.EntityID, &author, &c.Body, &metadata, &c.CreatedAt, &c.UpdatedAt); err != nil {
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
			sp.Comments = append(sp.Comments, c)
		}
	}
	return sp, nil
}

func (s *PostgresStore) ListSalesProcesses() ([]domain.SalesProcess, error) {
	rows, err := s.db.Query(`
		SELECT
			sp.id, sp.client_id, cl.name, cl.email, cl.phone, cl.source,
			cl.completed_at, sp.stage, sp.created_at,
			sp.initial_contact_date, sp.follow_up_date, sp.follow_up_result,
			sp.closed,
			CASE WHEN COALESCE(sp.closed, false) THEN sp.revenue ELSE NULL END,
			sp.stage_id, sp.lead_id
		FROM sales_process sp
		JOIN clients cl ON cl.id = sp.client_id
		WHERE COALESCE(sp.is_imported_placeholder, false) = false
		ORDER BY sp.created_at DESC, sp.id DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var processes []domain.SalesProcess
	var salesIDs []int
	idToIndex := make(map[int]int)

	for rows.Next() {
		var sp domain.SalesProcess
		var completedAt sql.NullTime
		var email, phone, source sql.NullString
		if err := rows.Scan(
			&sp.ID, &sp.ClientID, &sp.ClientName,
			&email, &phone, &source,
			&completedAt, &sp.Stage, &sp.CreatedAt,
			&sp.InitialContactDate, &sp.FollowUpDate, &sp.FollowUpResult,
			&sp.Closed, &sp.Revenue, &sp.StageID, &sp.LeadID,
		); err != nil {
			return nil, err
		}
		sp.ClientEmail = nullStringToPtr(email)
		sp.ClientPhone = nullStringToPtr(phone)
		sp.ClientSource = nullStringToPtr(source)
		sp.CompletedAt = nullTimeToString(completedAt, "2006-01-02")
		sp.Comments = []domain.Comment{}

		idToIndex[sp.ID] = len(processes)
		salesIDs = append(salesIDs, sp.ID)
		processes = append(processes, sp)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(salesIDs) > 0 {
		commentRows, err := s.db.Query(`
			SELECT id, entity_id, author, body, metadata, created_at, updated_at
			FROM comments
			WHERE entity_type = 'sales_process' AND entity_id = ANY($1)
			ORDER BY created_at DESC
		`, pq.Array(salesIDs))
		if err == nil {
			defer commentRows.Close()
			for commentRows.Next() {
				var c domain.Comment
				var author, metadata sql.NullString
				if err := commentRows.Scan(&c.ID, &c.EntityID, &author, &c.Body, &metadata, &c.CreatedAt, &c.UpdatedAt); err != nil {
					continue
				}
				c.EntityType = "sales_process"
				c.Author = nullStringToPtr(author)
				if metadata.Valid && metadata.String != "" {
					_ = json.Unmarshal([]byte(metadata.String), &c.Metadata)
				}
				if idx, ok := idToIndex[c.EntityID]; ok {
					processes[idx].Comments = append(processes[idx].Comments, c)
				}
			}
		}
	}
	return processes, nil
}
