// sales_start.go — helpers for StartSalesProcess: conflict detection and response building.
package api

import (
	"context"
	"strings"
	"time"

	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/domain"
)

func detectStartSalesConflicts(req StartSalesProcessRequest, existingClientID *int, existing domain.ClientBasic, foundLeadID *int) map[string]any {
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

		if req.Phone != "" && existing.Phone != "" &&
			normalize(req.Phone) != normalize(existing.Phone) {
			conflicts["phone"] = map[string]any{
				"existing": existing.Phone,
				"incoming": req.Phone,
			}
		}

		if req.Source != "" && existing.Source != "" &&
			normalize(req.Source) != normalize(existing.Source) {
			conflicts["source"] = map[string]any{
				"existing": existing.Source,
				"incoming": req.Source,
			}
		}
	}

	return conflicts
}

func (h *Handler) buildStartSalesProcessResponse(ctx context.Context, salesID, clientID int, stage string, req StartSalesProcessRequest, leadID *int) (StartSalesProcessResponse, error) {
	domComments, clientBasic, err := h.store.GetStartSalesResponseData(ctx, salesID, clientID)
	if err != nil {
		return StartSalesProcessResponse{}, err
	}

	respComments := make([]CommentResponse, 0, len(domComments))
	for _, c := range domComments {
		respComments = append(respComments, CommentResponse{
			ID:         c.ID,
			ClientID:   c.ClientID,
			EntityType: c.EntityType,
			EntityID:   c.EntityID,
			Author:     c.Author,
			Body:       c.Body,
			Metadata:   c.Metadata,
			CreatedAt:  c.CreatedAt.Format(time.RFC3339),
			UpdatedAt:  c.UpdatedAt.Format(time.RFC3339),
		})
	}

	respClient := ClientResponse{
		ID:            clientBasic.ID,
		Name:          clientBasic.Name,
		Email:         clientBasic.Email,
		Phone:         clientBasic.Phone,
		Source:        clientBasic.Source,
		SourceStageID: clientBasic.SourceStageID,
		Comments:      respComments,
	}

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
