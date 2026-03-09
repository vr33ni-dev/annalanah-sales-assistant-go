package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type startSalesExistingClient struct {
	ID     int
	Name   string
	Phone  sql.NullString
	Source sql.NullString
}

func (h *Handler) hasActiveContractForClient(ctx context.Context, clientID int) (bool, error) {
	var hasActiveContract bool
	err := h.DB.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM contracts
			WHERE client_id = $1
			  AND end_date >= CURRENT_DATE
		)
	`, clientID).Scan(&hasActiveContract)
	return hasActiveContract, err
}

func detectStartSalesConflicts(req StartSalesProcessRequest, existingClientID *int, existing startSalesExistingClient, foundLeadID *int) map[string]any {
	normalize := func(s string) string {
		return strings.ToLower(strings.TrimSpace(s))
	}

	conflicts := map[string]any{}
	if existingClientID != nil {
		if normalize(req.Name) != normalize(existing.Name) {
			conflicts["name"] = map[string]any{
				"existing": existing.Name,
				"incoming": req.Name,
			}
		}

		if req.Phone != "" && existing.Phone.Valid &&
			normalize(req.Phone) != normalize(existing.Phone.String) {
			conflicts["phone"] = map[string]any{
				"existing": existing.Phone.String,
				"incoming": req.Phone,
			}
		}

		if req.Source != "" && existing.Source.Valid &&
			normalize(req.Source) != normalize(existing.Source.String) {
			conflicts["source"] = map[string]any{
				"existing": existing.Source.String,
				"incoming": req.Source,
			}
		}
	}

	return conflicts
}

func deriveStartSalesStageAndStatus(req StartSalesProcessRequest) (string, string) {
	stage := "follow_up"
	if req.FollowUpDate != nil && strings.TrimSpace(*req.FollowUpDate) != "" {
		stage = "follow_up"
	} else if req.InitialContactDate != nil && strings.TrimSpace(*req.InitialContactDate) != "" {
		stage = "initial_contact"
	}

	desiredClientStatus := "initial_call_scheduled"
	if stage == "follow_up" {
		desiredClientStatus = "follow_up_scheduled"
	}

	return stage, desiredClientStatus
}

func (h *Handler) runStartSalesProcessTx(ctx context.Context, req StartSalesProcessRequest, existingClientID *int, foundLeadID *int) (int, int, string, *int, error) {
	stage, desiredClientStatus := deriveStartSalesStageAndStatus(req)
	effectiveLeadID := foundLeadID

	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, "", nil, fmt.Errorf("tx begin failed: %w", err)
	}
	defer tx.Rollback()

	var clientID int
	if existingClientID != nil {
		clientID = *existingClientID

		if req.MergeStrategy != nil && *req.MergeStrategy == "overwrite" {
			if _, err := tx.ExecContext(ctx, `
				UPDATE clients
				SET
					name   = COALESCE(NULLIF($1,''), name),
					phone  = COALESCE(NULLIF($2,''), phone),
					source = COALESCE(NULLIF($3,''), source)
				WHERE id = $4
			`, req.Name, req.Phone, req.Source, clientID); err != nil {
				return 0, 0, "", nil, fmt.Errorf("client overwrite failed: %w", err)
			}
		}
	} else {
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO clients (name, email, phone, source, source_stage_id, status)
			VALUES ($1,$2,$3,$4,$5,$6)
			RETURNING id
		`,
			req.Name, req.Email, req.Phone, req.Source, req.SourceStageID, desiredClientStatus,
		).Scan(&clientID); err != nil {
			return 0, 0, "", nil, fmt.Errorf("client insert failed: %w", err)
		}

		// Wire the new client to a lead record.
		if foundLeadID != nil {
			// A pre-existing lead was matched (by lead_id or email). Link it to the new client
			// without marking it converted — that only happens when a contract is signed.
			if _, err := tx.ExecContext(ctx, `
				UPDATE leads
				SET converted_client_id = $1
				WHERE id = $2
			`, clientID, *foundLeadID); err != nil {
				return 0, 0, "", nil, fmt.Errorf("lead link failed: %w", err)
			}
		} else {
			// No pre-existing lead — create a tracker lead linked to this client.
			// converted stays FALSE; it becomes TRUE only when a contract is signed.
			var newLeadID int
			leadInsertErr := tx.QueryRowContext(ctx, `
				INSERT INTO leads (name, email, phone, source, source_stage_id, converted, converted_client_id)
				VALUES ($1,$2,$3,$4,$5,FALSE,$6)
				ON CONFLICT (email) DO NOTHING
				RETURNING id
			`, req.Name, req.Email, req.Phone, req.Source, req.SourceStageID, clientID).Scan(&newLeadID)
			if leadInsertErr == nil {
				effectiveLeadID = &newLeadID
			} else if leadInsertErr == sql.ErrNoRows && strings.TrimSpace(req.Email) != "" {
				// Email already exists in leads (conflict) — link to that existing lead
				_ = tx.QueryRowContext(ctx,
					`SELECT id FROM leads WHERE LOWER(email) = LOWER($1) ORDER BY id DESC LIMIT 1`,
					req.Email,
				).Scan(&newLeadID)
				if newLeadID > 0 {
					effectiveLeadID = &newLeadID
				}
			}
		}
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE clients
		SET status = $1
		WHERE id = $2
		  AND status IS NULL
	`, desiredClientStatus, clientID); err != nil {
		return 0, 0, "", nil, fmt.Errorf("client status backfill failed: %w", err)
	}

	if req.MergeStrategy != nil && *req.MergeStrategy == "overwrite" && foundLeadID != nil {
		if _, err := tx.ExecContext(ctx, `
		UPDATE leads
		SET
			name   = COALESCE(NULLIF($1,''), name),
			email  = COALESCE(NULLIF($2,''), email),
			phone  = COALESCE(NULLIF($3,''), phone),
			source = COALESCE(NULLIF($4,''), source),
			source_stage_id = COALESCE($5, source_stage_id)
		WHERE id = $6
		  AND converted = FALSE
	`,
			req.Name,
			req.Email,
			req.Phone,
			req.Source,
			req.SourceStageID,
			*foundLeadID,
		); err != nil {
			return 0, 0, "", nil, fmt.Errorf("lead overwrite failed: %w", err)
		}
	}

	var salesID int
	err = tx.QueryRowContext(ctx, `
		INSERT INTO sales_process
			(client_id, initial_contact_date, follow_up_date, stage, stage_id, created_at, lead_id)
		VALUES ($1,$2,$3,$4,$5,now(),$6)
		ON CONFLICT (client_id) DO NOTHING
		RETURNING id
	`,
		clientID,
		req.InitialContactDate,
		req.FollowUpDate,
		stage,
		req.SourceStageID,
		effectiveLeadID,
	).Scan(&salesID)

	if err == sql.ErrNoRows {
		if err := tx.QueryRowContext(ctx,
			`SELECT id FROM sales_process WHERE client_id = $1`,
			clientID,
		).Scan(&salesID); err != nil {
			return 0, 0, "", nil, fmt.Errorf("sales_process reuse failed: %w", err)
		}
	} else if err != nil {
		return 0, 0, "", nil, fmt.Errorf("sales_process insert failed: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, "", nil, fmt.Errorf("commit failed: %w", err)
	}

	return clientID, salesID, stage, effectiveLeadID, nil
}

func (h *Handler) resolveLeadForSalesStart(ctx context.Context, req *StartSalesProcessRequest) (*int, error) {
	var foundLeadID *int

	if req.LeadID != nil {
		var converted bool
		if err := h.DB.QueryRowContext(ctx,
			`SELECT converted FROM leads WHERE id = $1`,
			*req.LeadID,
		).Scan(&converted); err == nil && !converted {
			foundLeadID = req.LeadID
		}
	}

	if foundLeadID == nil && strings.TrimSpace(req.Email) != "" {
		var id int
		if err := h.DB.QueryRowContext(ctx, `
			SELECT id FROM leads
			WHERE LOWER(email) = LOWER($1)
			  AND converted = FALSE
			ORDER BY id DESC
			LIMIT 1
		`, req.Email).Scan(&id); err == nil {
			foundLeadID = &id
		}
	}

	if foundLeadID != nil {
		var leadSource sql.NullString
		var leadSourceStage sql.NullInt64
		if err := h.DB.QueryRowContext(ctx, `SELECT source, source_stage_id FROM leads WHERE id = $1`, *foundLeadID).Scan(&leadSource, &leadSourceStage); err == nil {
			if req.SourceStageID == nil && leadSourceStage.Valid {
				v := int(leadSourceStage.Int64)
				req.SourceStageID = &v
			}
			if strings.TrimSpace(req.Source) == "" && leadSource.Valid {
				req.Source = leadSource.String
			}
		}
	}

	return foundLeadID, nil
}

func (h *Handler) resolveExistingClientForSalesStart(ctx context.Context, clientID *int) (*int, startSalesExistingClient, error) {
	var existingClientID *int
	var existing startSalesExistingClient

	if clientID != nil {
		existingClientID = clientID
		_ = h.DB.QueryRowContext(ctx,
			`SELECT name, phone, source FROM clients WHERE id = $1`,
			*clientID,
		).Scan(&existing.Name, &existing.Phone, &existing.Source)
		existing.ID = *clientID
	}

	return existingClientID, existing, nil
}

func (h *Handler) loadStartSalesProcessResponse(ctx context.Context, salesID, clientID int, stage string, req StartSalesProcessRequest, leadID *int) (StartSalesProcessResponse, error) {
	var respComments []CommentResponse
	commentRows, err := h.DB.QueryContext(ctx, `
		SELECT id, author, body, metadata, created_at, updated_at
		FROM comments
		WHERE entity_type = 'client' AND entity_id = $1
		ORDER BY created_at DESC
	`, clientID)
	if err == nil {
		defer commentRows.Close()
		for commentRows.Next() {
			var id int
			var author sql.NullString
			var body string
			var metadata sql.NullString
			var created, updated time.Time
			if err := commentRows.Scan(&id, &author, &body, &metadata, &created, &updated); err == nil {
				var meta map[string]interface{}
				if metadata.Valid && metadata.String != "" {
					_ = json.Unmarshal([]byte(metadata.String), &meta)
				}
				var a *string
				if author.Valid {
					s := author.String
					a = &s
				}
				respComments = append(respComments, CommentResponse{
					ID: id, EntityType: "client", EntityID: clientID, Author: a, Body: body, Metadata: meta,
					CreatedAt: created.Format(time.RFC3339), UpdatedAt: updated.Format(time.RFC3339),
				})
			}
		}
		if respComments == nil {
			respComments = []CommentResponse{}
		}
	}

	var respClient ClientResponse
	var phone sql.NullString
	var source sql.NullString
	var sourceStageID sql.NullInt64
	if err := h.DB.QueryRowContext(ctx, `
		SELECT id, name, email, phone, source, source_stage_id
		FROM clients
		WHERE id = $1
	`, clientID).Scan(
		&respClient.ID,
		&respClient.Name,
		&respClient.Email,
		&phone,
		&source,
		&sourceStageID,
	); err != nil {
		return StartSalesProcessResponse{}, err
	}
	if phone.Valid {
		respClient.Phone = phone.String
	}
	if source.Valid {
		respClient.Source = source.String
	}
	if sourceStageID.Valid {
		v := int(sourceStageID.Int64)
		respClient.SourceStageID = &v
	}
	respClient.Comments = respComments

	return StartSalesProcessResponse{
		SalesProcessID: salesID,
		Client:         respClient,
		SalesProcess: SalesProcessSummary{
			ID:                 salesID,
			ClientID:           clientID,
			Stage:              stage,
			InitialContactDate: req.InitialContactDate,
			FollowUpDate:       req.FollowUpDate,
			StageID:            req.SourceStageID,
			LeadID:             leadID,
		},
	}, nil
}
