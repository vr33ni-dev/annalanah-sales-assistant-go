package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/domain"
)

func (s *PostgresStore) ListCommentsByEntity(entityType string, entityID int) ([]domain.Comment, error) {
	rows, err := s.db.Query(`
		SELECT id, entity_type, entity_id, client_id, author, body, metadata, created_at, updated_at
		FROM comments
		WHERE entity_type = $1 AND entity_id = $2
		ORDER BY created_at DESC
	`, entityType, entityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCommentRows(rows)
}

func (s *PostgresStore) ListCommentsByClientID(clientID int) ([]domain.Comment, error) {
	rows, err := s.db.Query(`
		SELECT id, entity_type, entity_id, client_id, author, body, metadata, created_at, updated_at
		FROM comments
		WHERE client_id = $1
		ORDER BY created_at DESC
	`, clientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCommentRows(rows)
}

func scanCommentRows(rows *sql.Rows) ([]domain.Comment, error) {
	var out []domain.Comment
	for rows.Next() {
		var c domain.Comment
		var cid sql.NullInt64
		var author, metadata sql.NullString
		var created, updated sql.NullTime
		if err := rows.Scan(&c.ID, &c.EntityType, &c.EntityID, &cid, &author, &c.Body, &metadata, &created, &updated); err != nil {
			return nil, err
		}
		if cid.Valid {
			v := int(cid.Int64)
			c.ClientID = &v
		}
		if author.Valid {
			s := author.String
			c.Author = &s
		}
		if metadata.Valid && metadata.String != "" {
			_ = json.Unmarshal([]byte(metadata.String), &c.Metadata)
		}
		if created.Valid {
			c.CreatedAt = created.Time
		}
		if updated.Valid {
			c.UpdatedAt = updated.Time
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *PostgresStore) CreateComment(entityType string, entityID int, author *string, body string, metadata map[string]interface{}) (domain.Comment, error) {
	var clientID *int
	switch entityType {
	case "client":
		v := entityID
		clientID = &v
	case "sales_process":
		var cid int
		if err := s.db.QueryRow(`SELECT client_id FROM sales_process WHERE id = $1`, entityID).Scan(&cid); err == nil {
			clientID = &cid
		}
	case "contract":
		var cid int
		if err := s.db.QueryRow(`SELECT client_id FROM contracts WHERE id = $1`, entityID).Scan(&cid); err == nil {
			clientID = &cid
		}
	case "lead":
		var convertedClientID sql.NullInt64
		if err := s.db.QueryRow(`SELECT converted_client_id FROM leads WHERE id = $1`, entityID).Scan(&convertedClientID); err == nil && convertedClientID.Valid {
			cid := int(convertedClientID.Int64)
			clientID = &cid
		}
	}

	metaBytes, _ := json.Marshal(metadata)
	var id int
	var created, updated time.Time
	err := s.db.QueryRow(
		`INSERT INTO comments (entity_type, entity_id, client_id, author, body, metadata)
		 VALUES ($1,$2,$3,$4,$5,$6::jsonb) RETURNING id, created_at, updated_at`,
		entityType, entityID, clientID, author, body, string(metaBytes),
	).Scan(&id, &created, &updated)
	if err != nil {
		return domain.Comment{}, err
	}
	return domain.Comment{
		ID:         id,
		EntityType: entityType,
		EntityID:   entityID,
		ClientID:   clientID,
		Author:     author,
		Body:       body,
		Metadata:   metadata,
		CreatedAt:  created,
		UpdatedAt:  updated,
	}, nil
}

func (s *PostgresStore) DeleteComment(id int) error {
	result, err := s.db.Exec(`DELETE FROM comments WHERE id = $1`, id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) UpdateComment(id int, author *string, body *string, metadata *map[string]interface{}) (domain.Comment, error) {
	var sets []string
	var args []interface{}
	idx := 1
	if author != nil {
		sets = append(sets, fmt.Sprintf("author = $%d", idx))
		args = append(args, *author)
		idx++
	}
	if body != nil {
		sets = append(sets, fmt.Sprintf("body = $%d", idx))
		args = append(args, *body)
		idx++
	}
	if metadata != nil {
		metaBytes, _ := json.Marshal(*metadata)
		sets = append(sets, fmt.Sprintf("metadata = $%d::jsonb", idx))
		args = append(args, string(metaBytes))
		idx++
	}
	sets = append(sets, "updated_at = now()")
	args = append(args, id)

	query := fmt.Sprintf(
		`UPDATE comments SET %s WHERE id = $%d RETURNING id, entity_type, entity_id, author, body, metadata, created_at, updated_at`,
		strings.Join(sets, ", "), idx,
	)

	var (
		cid        int
		entityType string
		entityID   int
		authorNS   sql.NullString
		bodyStr    string
		metaNS     sql.NullString
		created    sql.NullTime
		updated    sql.NullTime
	)
	if err := s.db.QueryRow(query, args...).Scan(&cid, &entityType, &entityID, &authorNS, &bodyStr, &metaNS, &created, &updated); err != nil {
		if err == sql.ErrNoRows {
			return domain.Comment{}, ErrNotFound
		}
		return domain.Comment{}, err
	}

	var meta map[string]interface{}
	if metaNS.Valid && metaNS.String != "" {
		_ = json.Unmarshal([]byte(metaNS.String), &meta)
	}
	var a *string
	if authorNS.Valid {
		v := authorNS.String
		a = &v
	}
	var createdTime, updatedTime time.Time
	if created.Valid {
		createdTime = created.Time
	}
	if updated.Valid {
		updatedTime = updated.Time
	}
	return domain.Comment{
		ID:         cid,
		EntityType: entityType,
		EntityID:   entityID,
		Author:     a,
		Body:       bodyStr,
		Metadata:   meta,
		CreatedAt:  createdTime,
		UpdatedAt:  updatedTime,
	}, nil
}

func (s *PostgresStore) InsertCommentsForEntity(entityType string, entityID int, clientID int, comments []domain.Comment) error {
	for _, c := range comments {
		metaBytes, _ := json.Marshal(c.Metadata)
		if _, err := s.db.Exec(
			`INSERT INTO comments (entity_type, entity_id, client_id, author, body, metadata) VALUES ($1,$2,$3,$4,$5,$6::jsonb)`,
			entityType, entityID, clientID, c.Author, c.Body, string(metaBytes),
		); err != nil {
			return err
		}
	}
	return nil
}
