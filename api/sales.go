package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/lib/pq"
)

type SalesProcess struct {
	ID                 int      `json:"id"`
	ClientID           int      `json:"client_id"`
	Stage              string   `json:"stage"`
	InitialContactDate *string  `json:"initial_contact_date"`
	FollowUpDate       *string  `json:"follow_up_date"`
	FollowUpResult     *bool    `json:"follow_up_result"`
	Closed             *bool    `json:"closed"`
	Revenue            *float64 `json:"revenue"`
	StageID            *int     `json:"stage_id"`
	LeadID             *int     `json:"lead_id,omitempty"`
}

// What the API returns (GET /api/sales, PATCH /api/sales/{id})
type SalesProcessResponse struct {
	ID                 int               `json:"id"`
	ClientID           int               `json:"client_id"`
	ClientName         string            `json:"client_name"`
	ClientEmail        *string           `json:"client_email,omitempty"`
	ClientPhone        *string           `json:"client_phone,omitempty"`
	ClientSource       *string           `json:"client_source,omitempty"`
	CompletedAt        *string           `json:"completed_at"`
	Stage              string            `json:"stage"`
	CreatedAt          *string           `json:"created_at,omitempty"`
	UpdatedAt          *string           `json:"updated_at,omitempty"`
	InitialContactDate *string           `json:"initial_contact_date"`
	FollowUpDate       *string           `json:"follow_up_date"`
	FollowUpResult     *bool             `json:"follow_up_result"`
	Closed             *bool             `json:"closed"`
	Revenue            *float64          `json:"revenue"`
	StageID            *int              `json:"stage_id"`
	LeadID             *int              `json:"lead_id,omitempty"`
	Comments           []CommentResponse `json:"comments,omitempty"`
}

// What the API accepts (PATCH /api/sales/{id})
type SalesProcessUpdateRequest struct {
	InitialContactDate     *string                `json:"initial_contact_date,omitempty"`
	FollowUpDate           *string                `json:"follow_up_date,omitempty"`
	FollowUpResult         *bool                  `json:"follow_up_result"`
	Closed                 *bool                  `json:"closed"`
	Revenue                *float64               `json:"revenue"`
	ContractDurationMonths *int                   `json:"contract_duration_months,omitempty"`
	ContractStartDate      *string                `json:"contract_start_date,omitempty"`
	ContractFrequency      *string                `json:"contract_frequency,omitempty"`
	CompletedAt            *string                `json:"completed_at,omitempty"`
	Comments               []CommentCreateRequest `json:"comments,omitempty"`
}

// GET /api/sales
func (h *Handler) ListSalesProcesses(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.Query(`
	SELECT
		sp.id,
		sp.client_id,
		cl.name  AS client_name,
		cl.email AS client_email,
		cl.phone AS client_phone,
		cl.source AS client_source,
		cl.completed_at AS completed_at,
		sp.stage,
		sp.created_at,
		sp.initial_contact_date,
		sp.follow_up_date,
		sp.follow_up_result,
		sp.closed,
		CASE WHEN COALESCE(sp.closed, false) THEN sp.revenue ELSE NULL END AS revenue,
		sp.stage_id,
		sp.lead_id
	FROM sales_process sp
	JOIN clients cl ON cl.id = sp.client_id
	WHERE COALESCE(sp.is_imported_placeholder, false) = false
	ORDER BY sp.created_at DESC, sp.id DESC
`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var processes []SalesProcessResponse
	var salesIDs []int
	idToIndex := make(map[int]int)

	for rows.Next() {
		var sp SalesProcessResponse
		var completedAt sql.NullTime
		if err := rows.Scan(
			&sp.ID,
			&sp.ClientID,
			&sp.ClientName,
			&sp.ClientEmail,
			&sp.ClientPhone,
			&sp.ClientSource,
			&completedAt,
			&sp.Stage,
			&sp.CreatedAt,
			&sp.InitialContactDate,
			&sp.FollowUpDate,
			&sp.FollowUpResult,
			&sp.Closed,
			&sp.Revenue,
			&sp.StageID,
			&sp.LeadID,
		); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if completedAt.Valid {
			s := completedAt.Time.Format("2006-01-02")
			sp.CompletedAt = &s
		}

		sp.Comments = []CommentResponse{}

		idToIndex[sp.ID] = len(processes)
		salesIDs = append(salesIDs, sp.ID)
		processes = append(processes, sp)
	}

	// Batch load sales_process comments (avoid N+1)
	if len(salesIDs) > 0 {
		commentRows, err := h.DB.Query(`
			SELECT id, entity_id, author, body, metadata, created_at, updated_at
			FROM comments
			WHERE entity_type = 'sales_process'
			  AND entity_id = ANY($1)
			ORDER BY created_at DESC
		`, pq.Array(salesIDs))

		if err == nil {
			defer commentRows.Close()

			for commentRows.Next() {
				var id int
				var entityID int
				var author sql.NullString
				var body string
				var metadata sql.NullString
				var created, updated time.Time

				if err := commentRows.Scan(
					&id,
					&entityID,
					&author,
					&body,
					&metadata,
					&created,
					&updated,
				); err != nil {
					continue
				}

				var meta map[string]interface{}
				if metadata.Valid && metadata.String != "" {
					_ = json.Unmarshal([]byte(metadata.String), &meta)
				}

				var a *string
				if author.Valid {
					s := author.String
					a = &s
				}

				if idx, ok := idToIndex[entityID]; ok {
					processes[idx].Comments = append(processes[idx].Comments, CommentResponse{
						ID:         id,
						EntityType: "sales_process",
						EntityID:   entityID,
						Author:     a,
						Body:       body,
						Metadata:   meta,
						CreatedAt:  created.Format(time.RFC3339),
						UpdatedAt:  updated.Format(time.RFC3339),
					})
				}
			}
		}
	}

	if processes == nil {
		processes = []SalesProcessResponse{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(processes)
}

// PATCH /api/sales/{id}
// Orchestrates update -> sync -> ensure contract -> load response -> maybe cleanup -> encode JSON.
func (h *Handler) UpdateSalesProcess(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid sales process id", http.StatusBadRequest)
		return
	}

	// Use the update request type that can carry contract details
	var sp SalesProcessUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&sp); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// ---------- NORMALIZATION ----------
	// If the follow-up did not happen (no-show), the process cannot be closed/won.
	// Force closed=false and clear any won-only fields.
	if sp.FollowUpResult != nil && *sp.FollowUpResult == false {
		f := false
		sp.Closed = &f
		sp.Revenue = nil
		sp.ContractDurationMonths = nil
		sp.ContractStartDate = nil
		sp.ContractFrequency = nil
		sp.CompletedAt = nil
	}

	// ---------- VALIDATION ----------
	// If closed=true, all contract fields must be present/valid.
	if sp.Closed != nil && *sp.Closed == true {
		if sp.Revenue == nil ||
			sp.ContractDurationMonths == nil || *sp.ContractDurationMonths <= 0 ||
			sp.ContractStartDate == nil ||
			sp.ContractFrequency == nil ||
			(*sp.ContractFrequency != "monthly" && *sp.ContractFrequency != "bi-monthly" && *sp.ContractFrequency != "quarterly" && *sp.ContractFrequency != "one-time" && *sp.ContractFrequency != "bi-yearly") {
			http.Error(w, "cannot set closed=true without contract details (revenue, duration>0, start date, frequency)", http.StatusBadRequest)
			return
		}
	}

	// Small ergonomics: if closed=true but result wasn’t provided, assume the call happened
	if sp.Closed != nil && *sp.Closed == true && sp.FollowUpResult == nil {
		t := true
		sp.FollowUpResult = &t
	}

	// ---------- UPDATE SALES_PROCESS (fields + normalized stage) ----------
	_, err = h.DB.Exec(`
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
		stage = CASE
			-- A no-show ends the process (UI can’t reschedule), mark as lost.
			WHEN COALESCE($3, follow_up_result) IS FALSE THEN 'lost'
			WHEN COALESCE($4, closed) IS TRUE  THEN 'closed'
			WHEN $4 IS NOT NULL AND $4 IS FALSE THEN 'lost'
			WHEN stage = 'lost' AND $4 IS NULL AND $3 IS NULL THEN 'lost'
			WHEN COALESCE($2, follow_up_date) IS NOT NULL THEN 'follow_up'
			WHEN COALESCE($1, initial_contact_date) IS NOT NULL THEN 'initial_contact'
			WHEN COALESCE($3, follow_up_result) IS TRUE  THEN 'follow_up'
			ELSE 'follow_up'
		END
	WHERE id = $6
`,
		sp.InitialContactDate,
		sp.FollowUpDate,
		sp.FollowUpResult,
		sp.Closed,
		sp.Revenue,
		id,
	)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Resolve client_id once (used for completed_at and optional contract creation)
	var clientID int
	if err := h.DB.QueryRow(`SELECT client_id FROM sales_process WHERE id = $1`, id).Scan(&clientID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := h.syncClientCompletedAtFromSales(r.Context(), clientID, sp); err != nil {
		http.Error(w, "failed to sync completed_at: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := h.syncClientStatusFromSales(r.Context(), id); err != nil {
		http.Error(w, "failed to sync client status: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := h.ensureContractForClosedSales(r.Context(), id, clientID, sp); err != nil {
		if errors.Is(err, errInvalidContractStartDate) {
			http.Error(w, "invalid contract_start_date", http.StatusBadRequest)
			return
		}
		http.Error(w, "failed to ensure contract for closed sales: "+err.Error(), http.StatusInternalServerError)
		return
	}

	updated, err := h.loadSalesProcessResponse(id)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "sales process not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// If this sales process was started from an unconverted lead and ended up lost,
	// delete the temporary client record so the lead can be scheduled again cleanly.
	if updated.Stage == "lost" && updated.Closed != nil && !*updated.Closed {
		shouldDelete, err := h.shouldDeleteLostTemporaryClient(r.Context(), updated.ID, updated.ClientID)
		if err != nil {
			http.Error(w, "cleanup check failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if shouldDelete {
			if err := h.deleteTemporaryClientWithComments(r.Context(), updated.ClientID, updated.ID); err != nil {
				http.Error(w, "temporary client cleanup failed: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(updated)
}

// POST /api/sales/start
type StartSalesProcessRequest struct {
	Name               string                 `json:"name"`
	Email              string                 `json:"email"`
	Phone              string                 `json:"phone"`
	Source             string                 `json:"source"`
	SourceStageID      *int                   `json:"source_stage_id,omitempty"`
	InitialContactDate *string                `json:"initial_contact_date,omitempty"`
	FollowUpDate       *string                `json:"follow_up_date"`
	LeadID             *int                   `json:"lead_id,omitempty"`
	MergeStrategy      *string                `json:"merge_strategy,omitempty"` // overwrite | keep_existing
	ClientID           *int                   `json:"client_id,omitempty"`
	Comments           []CommentCreateRequest `json:"comments,omitempty"`
}

// ClientResponse is the nested client object returned inside StartSalesProcessResponse.
// It intentionally contains a compact subset of client fields and any comments.
type ClientResponse struct {
	ID            int               `json:"id"`
	Name          string            `json:"name"`
	Email         string            `json:"email"`
	Phone         string            `json:"phone"`
	Source        string            `json:"source"`
	SourceStageID *int              `json:"source_stage_id,omitempty"`
	Comments      []CommentResponse `json:"comments,omitempty"`
}

// SalesProcessSummary is the nested sales-process object returned inside StartSalesProcessResponse.
// It represents a compact summary of the newly created sales process.
type SalesProcessSummary struct {
	ID                 int     `json:"id"`
	ClientID           int     `json:"client_id"`
	Stage              string  `json:"stage"`
	InitialContactDate *string `json:"initial_contact_date"`
	FollowUpDate       *string `json:"follow_up_date"`
	FollowUpResult     *bool   `json:"follow_up_result"`
	Closed             *bool   `json:"closed"`
	Revenue            *int    `json:"revenue"`
	StageID            *int    `json:"stage_id"`
	LeadID             *int    `json:"lead_id,omitempty"`
}

type StartSalesProcessResponse struct {
	SalesProcessID int                 `json:"sales_process_id"`
	Client         ClientResponse      `json:"client"`
	SalesProcess   SalesProcessSummary `json:"sales_process"`
}

func (h *Handler) StartSalesProcess(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// ------------------------------------------------
	// 0) Parse & validate request
	// ------------------------------------------------
	var req StartSalesProcessRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.InitialContactDate == nil || strings.TrimSpace(*req.InitialContactDate) == "" {
		http.Error(w, "initial_contact_date is required", http.StatusBadRequest)
		return
	}

	// ------------------------------------------------
	// 1) Resolve lead (ignore converted)
	// ------------------------------------------------
	foundLeadID, err := h.resolveLeadForSalesStart(ctx, &req)
	if err != nil {
		http.Error(w, "lead resolution failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// ------------------------------------------------
	// 2) Resolve existing client (PIN via client_id if present)
	// ------------------------------------------------
	existingClientID, existing, err := h.resolveExistingClientForSalesStart(ctx, req.ClientID)
	if err != nil {
		http.Error(w, "existing client resolution failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// ------------------------------------------------
	// 3) ABSOLUTE HARD STOP: active contract
	// ------------------------------------------------
	if existingClientID != nil {
		hasActiveContract, err := h.hasActiveContractForClient(ctx, *existingClientID)
		if err != nil {
			http.Error(w, "contract lookup failed: "+err.Error(), http.StatusInternalServerError)
			return
		}

		if hasActiveContract {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":     "client_has_active_contract",
				"client_id": *existingClientID,
			})
			return // ABSOLUTE STOP
		}

	}

	// ------------------------------------------------
	// 4) Detect conflicts
	// ------------------------------------------------
	conflicts := detectStartSalesConflicts(req, existingClientID, existing, foundLeadID)

	// ------------------------------------------------
	// 5) Merge decision required
	// ------------------------------------------------
	// If the caller selected an existing lead, prefer allowing the incoming
	// non-empty fields to update the existing client (merge/overwrite)
	// so the UI can adjust e.g. `source` while reusing the client.
	if existingClientID != nil && foundLeadID != nil && req.MergeStrategy == nil {
		ov := "overwrite"
		req.MergeStrategy = &ov
	}
	if existingClientID != nil &&
		len(conflicts) > 0 &&
		req.MergeStrategy == nil {

		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":               "client_exists",
			"client_id":           *existingClientID,
			"has_active_contract": false,
			"conflicts":           conflicts,
			"original_payload":    req,
		})
		return
	}

	clientID, salesID, stage, effectiveLeadID, err := h.runStartSalesProcessTx(ctx, req, existingClientID, foundLeadID)
	if err != nil {
		if isUniqueViolation(err, "unique_client_email") {
			writeJSONError(w, "Ein Kunde mit dieser E-Mail-Adresse existiert bereits. Bitte den bestehenden Kunden auswählen.", http.StatusConflict)
			return
		}
		msg := err.Error()
		http.Error(w, msg, http.StatusInternalServerError)
		return
	}

	// optionally insert comments provided with the create request (post-commit)
	if len(req.Comments) > 0 {
		if err := h.insertCommentsForEntity("client", clientID, req.Comments); err != nil {
			// ignore insertion error for now
		}
	}

	resp, err := h.loadStartSalesProcessResponse(ctx, salesID, clientID, stage, req, effectiveLeadID)
	if err != nil {
		http.Error(w, "start sales response load failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)

}

// createClientAndSalesProcessTx creates a client and a follow-up sales_process inside the provided tx.
// Returns clientID, salesProcessID or error. leadID may be nil.
func (h *Handler) createClientAndSalesProcessTx(
	ctx context.Context,
	tx *sql.Tx,
	name string,
	email *string,
	phone *string,
	source string,
	sourceStageID *int,
	initialContactDate *string,
	followUpDate *string,
	leadID *int,
) (int, int, error) {

	var clientID int
	var salesProcessID int

	// determine stage in Go to avoid SQL parameter type ambiguity
	stage := "follow_up"
	if followUpDate != nil && strings.TrimSpace(*followUpDate) != "" {
		stage = "follow_up"
	} else if initialContactDate != nil && strings.TrimSpace(*initialContactDate) != "" {
		stage = "initial_contact"
	}

	clientStatus := "initial_call_scheduled"
	if stage == "follow_up" {
		clientStatus = "follow_up_scheduled"
	}

	// ------------------------------------------
	// 1) Create OR reuse client (email-idempotent)
	// ------------------------------------------
	if email != nil && *email != "" {
		err := tx.QueryRowContext(ctx,
			`SELECT id FROM clients WHERE LOWER(email) = LOWER($1)`,
			*email,
		).Scan(&clientID)

		if err == sql.ErrNoRows {
			err = tx.QueryRowContext(ctx,
				`INSERT INTO clients (name, email, phone, source, source_stage_id, status)
				 VALUES ($1,$2,$3,$4,$5,$6)
				 RETURNING id`,
				name, email, phone, source, sourceStageID, clientStatus,
			).Scan(&clientID)
			if err != nil {
				return 0, 0, err
			}
		} else if err != nil {
			return 0, 0, err
		}
	} else {
		// no email → always new client
		err := tx.QueryRowContext(ctx,
			`INSERT INTO clients (name, phone, source, source_stage_id, status)
			 VALUES ($1,$2,$3,$4,$5)
			 RETURNING id`,
			name, phone, source, sourceStageID, clientStatus,
		).Scan(&clientID)
		if err != nil {
			return 0, 0, err
		}
	}

	// ------------------------------------------
	// 2) Create OR reuse sales_process (SAFE)
	// ------------------------------------------

	err := tx.QueryRowContext(ctx,
		`INSERT INTO sales_process
			(client_id, initial_contact_date, follow_up_date, stage, stage_id, created_at, lead_id)
		 VALUES ($1,$2,$3,$4,$5,now(),$6)
		 ON CONFLICT (client_id)
		 DO NOTHING
		 RETURNING id`,
		clientID,
		initialContactDate,
		followUpDate,
		stage,
		sourceStageID,
		leadID,
	).Scan(&salesProcessID)

	if err == sql.ErrNoRows {
		// conflict happened → reuse existing sales_process
		if err := tx.QueryRowContext(ctx,
			`SELECT id FROM sales_process WHERE client_id = $1`,
			clientID,
		).Scan(&salesProcessID); err != nil {
			return 0, 0, err
		}
	} else if err != nil {
		return 0, 0, err
	}

	return clientID, salesProcessID, nil
}

// Upsells
// Using Pointers because PostgreSQL can return NULL, and Go's plain string cannot represent NULL, only "" and these fields can return nil
type ContractUpsell struct {
	ID                     int        `json:"id"`
	SalesProcessID         int        `json:"sales_process_id"`
	ClientID               int        `json:"client_id"`
	UpsellDate             *string    `json:"upsell_date"`
	UpsellResult           *string    `json:"upsell_result"` // "verlaengerung" or "keine_verlaengerung"`
	UpsellRevenue          *float64   `json:"upsell_revenue,omitempty"`
	ContractStartDate      *time.Time `json:"contract_start_date"`
	ContractDurationMonths *int       `json:"contract_duration_months"`
	ContractFrequency      *string    `json:"contract_frequency"`
	PreviousContractID     *int       `json:"previous_contract_id,omitempty"`
	NewContractID          *int       `json:"new_contract_id,omitempty"`
	CreatedAt              *string    `json:"created_at"`
	UpdatedAt              *string    `json:"updated_at"`
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanContractUpsell(scanner rowScanner, u *ContractUpsell) error {
	var (
		upsellDate             sql.NullTime
		upsellResult           sql.NullString
		upsellRevenue          sql.NullFloat64
		previousContractID     sql.NullInt64
		newContractID          sql.NullInt64
		createdAt              sql.NullTime
		updatedAt              sql.NullTime
		contractStartDate      sql.NullTime
		contractDurationMonths sql.NullInt64
		contractFrequency      sql.NullString
	)

	if err := scanner.Scan(
		&u.ID,
		&u.SalesProcessID,
		&u.ClientID,
		&upsellDate,
		&upsellResult,
		&upsellRevenue,
		&previousContractID,
		&newContractID,
		&createdAt,
		&updatedAt,
		&contractStartDate,
		&contractDurationMonths,
		&contractFrequency,
	); err != nil {
		return err
	}

	if upsellDate.Valid {
		s := upsellDate.Time.Format("2006-01-02")
		u.UpsellDate = &s
	} else {
		u.UpsellDate = nil
	}

	if upsellResult.Valid {
		s := upsellResult.String
		u.UpsellResult = &s
	} else {
		u.UpsellResult = nil
	}

	if upsellRevenue.Valid {
		f := upsellRevenue.Float64
		u.UpsellRevenue = &f
	} else {
		u.UpsellRevenue = nil
	}

	if previousContractID.Valid {
		id := int(previousContractID.Int64)
		u.PreviousContractID = &id
	} else {
		u.PreviousContractID = nil
	}

	if newContractID.Valid {
		id := int(newContractID.Int64)
		u.NewContractID = &id
	} else {
		u.NewContractID = nil
	}

	if createdAt.Valid {
		s := createdAt.Time.Format(time.RFC3339)
		u.CreatedAt = &s
	} else {
		u.CreatedAt = nil
	}

	if updatedAt.Valid {
		s := updatedAt.Time.Format(time.RFC3339)
		u.UpdatedAt = &s
	} else {
		u.UpdatedAt = nil
	}

	if contractStartDate.Valid {
		t := contractStartDate.Time
		u.ContractStartDate = &t
	} else {
		u.ContractStartDate = nil
	}

	if contractDurationMonths.Valid {
		months := int(contractDurationMonths.Int64)
		u.ContractDurationMonths = &months
	} else {
		u.ContractDurationMonths = nil
	}

	if contractFrequency.Valid {
		s := contractFrequency.String
		u.ContractFrequency = &s
	} else {
		u.ContractFrequency = nil
	}

	return nil
}

type CreateUpsellRequest struct {
	UpsellDate             json.RawMessage `json:"upsell_date"`
	UpsellResult           *string         `json:"upsell_result,omitempty"`
	UpsellRevenue          *float64        `json:"upsell_revenue,omitempty"`
	ContractStartDate      *string         `json:"contract_start_date,omitempty"`
	ContractDurationMonths *int            `json:"contract_duration_months,omitempty"`
	ContractFrequency      *string         `json:"contract_frequency,omitempty"`
}

func (h *Handler) GetUpsellForSalesProcess(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	salesID, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid sales process id", http.StatusBadRequest)
		return
	}

	rows, err := h.DB.Query(`
SELECT 
    cu.id,
    cu.sales_process_id,
    cu.client_id,
    cu.upsell_date,
    cu.upsell_result,
    cu.upsell_revenue,
    cu.previous_contract_id,
    cu.new_contract_id,
    cu.created_at,
    cu.updated_at,
    c.start_date AS contract_start_date,
    c.duration_months AS contract_duration_months,
    c.payment_frequency AS contract_frequency
FROM contract_upsells cu
LEFT JOIN contracts c
       ON c.id = cu.new_contract_id
WHERE cu.sales_process_id = $1
ORDER BY cu.upsell_date DESC NULLS LAST, cu.id DESC
`, salesID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	var list []ContractUpsell

	for rows.Next() {
		var u ContractUpsell
		if err := scanContractUpsell(rows, &u); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		list = append(list, u)
	}

	if err := rows.Err(); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	if list == nil {
		list = []ContractUpsell{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(list)
}

func (h *Handler) ListUpsellCategories(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var where []string
	var args []any
	idx := 1

	if v := q.Get("start_date"); v != "" {
		start, err := time.Parse("2006-01-02", v)
		if err != nil {
			http.Error(w, "invalid start_date (expected YYYY-MM-DD)", http.StatusBadRequest)
			return
		}
		where = append(where, "cu.upsell_date >= $"+strconv.Itoa(idx))
		args = append(args, start)
		idx++
	}
	if v := q.Get("end_date"); v != "" {
		end, err := time.Parse("2006-01-02", v)
		if err != nil {
			http.Error(w, "invalid end_date (expected YYYY-MM-DD)", http.StatusBadRequest)
			return
		}
		where = append(where, "cu.upsell_date <= $"+strconv.Itoa(idx))
		args = append(args, end)
		idx++
	}

	whereSQL := ""
	if len(where) > 0 {
		whereSQL = "WHERE " + strings.Join(where, " AND ")
	}

	query := `
SELECT 
    cu.id,
    cu.sales_process_id,
    cu.client_id,
    cu.upsell_date,
    cu.upsell_result,
    cu.upsell_revenue,
    cu.previous_contract_id,
    cu.new_contract_id,
    cu.created_at,
    cu.updated_at,
    c.start_date AS contract_start_date,
    c.duration_months AS contract_duration_months,
    c.payment_frequency AS contract_frequency
FROM contract_upsells cu
LEFT JOIN contracts c
       ON c.id = cu.new_contract_id
` + whereSQL + `
ORDER BY cu.upsell_date DESC NULLS LAST, cu.id DESC
`

	rows, err := h.DB.Query(query, args...)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	var scheduled []ContractUpsell
	var successful []ContractUpsell
	var unsuccessful []ContractUpsell

	for rows.Next() {
		var u ContractUpsell
		if err := scanContractUpsell(rows, &u); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		if u.UpsellResult == nil {
			scheduled = append(scheduled, u)
		} else if *u.UpsellResult == "verlaengerung" {
			successful = append(successful, u)
		} else if *u.UpsellResult == "keine_verlaengerung" {
			unsuccessful = append(unsuccessful, u)
		}
	}

	if err := rows.Err(); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	resp := map[string]any{
		"scheduled":    scheduled,
		"successful":   successful,
		"unsuccessful": unsuccessful,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *Handler) CreateOrUpdateUpsell(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	salesID, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid sales process id", http.StatusBadRequest)
		return
	}

	var req CreateUpsellRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Resolve upsell_date: nil RawMessage = field absent (don't touch on UPDATE);
	// RawMessage "null" = explicit clear; otherwise parse as date string.
	var resolvedUpsellDate *string
	upsellDateProvided := req.UpsellDate != nil
	if upsellDateProvided && string(req.UpsellDate) != "null" {
		var d string
		if err := json.Unmarshal(req.UpsellDate, &d); err != nil {
			http.Error(w, "invalid upsell_date", http.StatusBadRequest)
			return
		}
		resolvedUpsellDate = &d
	}

	// -----------------------
	// VALIDATION
	// -----------------------

	// Validate upsell_result only if provided
	if req.UpsellResult != nil {
		if *req.UpsellResult != "verlaengerung" && *req.UpsellResult != "keine_verlaengerung" {
			http.Error(w, "upsell_result must be 'verlaengerung' or 'keine_verlaengerung'", http.StatusBadRequest)
			return
		}
	}

	// If verlängerung → revenue is required
	if req.UpsellResult != nil && *req.UpsellResult == "verlaengerung" {
		if req.UpsellRevenue == nil {
			http.Error(w, "upsell_revenue required for verlängerung", http.StatusBadRequest)
			return
		}
	}

	// -----------------------
	// Resolve client_id
	// -----------------------

	var clientID int
	err = h.DB.QueryRow(`SELECT client_id FROM sales_process WHERE id = $1`, salesID).Scan(&clientID)
	if err != nil {
		http.Error(w, "sales process not found", http.StatusNotFound)
		return
	}

	tx, err := h.DB.Begin()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer tx.Rollback()

	// -----------------------
	// Check if there is an existing open (pending) upsell
	// pending = has date but no result yet
	// -----------------------

	var existingUpsellID *int
	err = tx.QueryRow(`
			SELECT id FROM contract_upsells
			WHERE sales_process_id = $1
				AND upsell_result IS NULL
			ORDER BY upsell_date DESC NULLS LAST, id DESC
			LIMIT 1
	`, salesID).Scan(&existingUpsellID)

	if err == sql.ErrNoRows {
		existingUpsellID = nil
	} else if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	// -----------------------
	// Determine active previous contract (may be null)
	// -----------------------

	var prevContractID *int
	_ = tx.QueryRow(`
        SELECT id FROM contracts
        WHERE client_id = $1
        ORDER BY start_date DESC, id DESC LIMIT 1
    `, clientID).Scan(&prevContractID)

	// -----------------------
	// If verlängerung → create new contract + cashflow
	// -----------------------

	var newContractID *int = nil
	var notifyRevenue *float64
	var notifyStartDate *time.Time
	var notifySalesProcessID *int

	if req.UpsellResult != nil && *req.UpsellResult == "verlaengerung" {

		// ----- VALIDATE required contract fields -----
		if req.ContractStartDate == nil ||
			req.ContractDurationMonths == nil || *req.ContractDurationMonths <= 0 ||
			req.ContractFrequency == nil {

			http.Error(w, "contract_start_date, contract_duration_months > 0 and contract_frequency (monthly|bi-monthly|quarterly|one-time|bi-yearly) are required", http.StatusBadRequest)
			return
		}

		pf, err := normalizePaymentFrequency(*req.ContractFrequency, *req.ContractDurationMonths)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		sd, err := time.Parse("2006-01-02", *req.ContractStartDate)
		if err != nil {
			http.Error(w, "invalid contract_start_date", 400)
			return
		}

		spID := salesID
		contractID, _, err := h.createContractTx(r.Context(), tx, ContractCreateInput{
			ClientID:         clientID,
			SalesProcessID:   &spID,
			StartDate:        sd,
			DurationMonths:   *req.ContractDurationMonths,
			RevenueTotal:     *req.UpsellRevenue,
			PaymentFreq:      pf,
			GenerateSchedule: true,
		})
		if err != nil {
			http.Error(w, "failed to create contract: "+err.Error(), 500)
			return
		}
		newContractID = &contractID
		notifyRevenue = req.UpsellRevenue
		notifyStartDate = &sd
		notifySalesProcessID = &spID

	}

	// -----------------------
	// Insert or update upsell
	// -----------------------

	var upsellID int

	if existingUpsellID == nil {
		// CREATE
		err = tx.QueryRow(`
            INSERT INTO contract_upsells
                (sales_process_id, client_id, upsell_date, upsell_result,
                 upsell_revenue, previous_contract_id, new_contract_id)
            VALUES ($1,$2,$3,$4,$5,$6,$7)
            RETURNING id
        `,
			salesID,
			clientID,
			resolvedUpsellDate, // nil if absent or explicit null
			req.UpsellResult,
			req.UpsellRevenue,
			prevContractID,
			newContractID,
		).Scan(&upsellID)
	} else {
		// UPDATE
		err = tx.QueryRow(`
            UPDATE contract_upsells
            SET
                upsell_date     = CASE WHEN $2 THEN $3 ELSE upsell_date END,
                upsell_result   = COALESCE($4, upsell_result),
                upsell_revenue  = COALESCE($5, upsell_revenue),
                new_contract_id = COALESCE($6, new_contract_id)
            WHERE id = $1
            RETURNING id
        `,
			*existingUpsellID,
			upsellDateProvided, // true = apply $3; false = keep existing
			resolvedUpsellDate, // nil clears the date, string sets it
			req.UpsellResult,
			req.UpsellRevenue,
			newContractID,
		).Scan(&upsellID)
	}

	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, "commit: "+err.Error(), 500)
		return
	}

	if newContractID != nil && notifyRevenue != nil && notifyStartDate != nil && notifySalesProcessID != nil {
		h.notifyNewContractAsync(*newContractID, clientID, *notifyRevenue, *notifyStartDate, notifySalesProcessID)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"upsell_id":       upsellID,
		"updated":         existingUpsellID != nil,
		"new_contract_id": newContractID,
	})
}

func (h *Handler) GetUpsellAnalytics(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var where []string
	var args []any
	idx := 1

	if v := q.Get("start_date"); v != "" {
		start, err := time.Parse("2006-01-02", v)
		if err != nil {
			http.Error(w, "invalid start_date (expected YYYY-MM-DD)", http.StatusBadRequest)
			return
		}
		where = append(where, "cu.upsell_date >= $"+strconv.Itoa(idx))
		args = append(args, start)
		idx++
	}
	if v := q.Get("end_date"); v != "" {
		end, err := time.Parse("2006-01-02", v)
		if err != nil {
			http.Error(w, "invalid end_date (expected YYYY-MM-DD)", http.StatusBadRequest)
			return
		}
		where = append(where, "cu.upsell_date <= $"+strconv.Itoa(idx))
		args = append(args, end)
		idx++
	}

	whereSQL := ""
	if len(where) > 0 {
		whereSQL = "WHERE " + strings.Join(where, " AND ")
	}

	var stats struct {
		VerlaengerungCount      int      `json:"verlaengerung_count"`
		KeineVerlaengerungCount int      `json:"keine_verlaengerung_count"`
		ScheduledCount          int      `json:"scheduled_count"`
		Verlaengerungsquote     *float64 `json:"verlaengerungsquote"`
		UmsatzSum               float64  `json:"umsatz_sum"`
	}

	query := `
        SELECT
			COUNT(*) FILTER (WHERE upsell_result = 'verlaengerung')         AS verlaengerung_count,
			COUNT(*) FILTER (WHERE upsell_result = 'keine_verlaengerung')  AS keine_verlaengerung_count,
			COUNT(*) FILTER (WHERE upsell_result IS NULL)                  AS scheduled_count,

			ROUND(
				100.0 * COUNT(*) FILTER (WHERE upsell_result = 'verlaengerung')
				/ NULLIF(
					COUNT(*) FILTER (WHERE upsell_result IN ('verlaengerung','keine_verlaengerung')),
					0
				),
				1
			) AS verlaengerungsquote,

			COALESCE(SUM(upsell_revenue), 0) AS umsatz_sum
		FROM contract_upsells cu
	` + whereSQL + `;
    `

	err := h.DB.QueryRow(query, args...).Scan(
		&stats.VerlaengerungCount,
		&stats.KeineVerlaengerungCount,
		&stats.ScheduledCount,
		&stats.Verlaengerungsquote,
		&stats.UmsatzSum,
	)

	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	// Monthly renewal revenue breakdown
	renewalWhere := append(where, "cu.upsell_result = 'verlaengerung'", "cu.upsell_date IS NOT NULL")
	renewalWhereSQL := "WHERE " + strings.Join(renewalWhere, " AND ")

	monthlyQuery := `
		SELECT
			TO_CHAR(cu.upsell_date, 'YYYY-MM') AS month,
			COALESCE(SUM(cu.upsell_revenue), 0) AS revenue
		FROM contract_upsells cu
		` + renewalWhereSQL + `
		GROUP BY month
		ORDER BY month
	`

	type monthlyRevenue struct {
		Month   string  `json:"month"`
		Revenue float64 `json:"revenue"`
	}

	rows, err := h.DB.Query(monthlyQuery, args...)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	revenueByMonth := []monthlyRevenue{}
	for rows.Next() {
		var row monthlyRevenue
		if err := rows.Scan(&row.Month, &row.Revenue); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		revenueByMonth = append(revenueByMonth, row)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"verlaengerung_count":       stats.VerlaengerungCount,
		"keine_verlaengerung_count": stats.KeineVerlaengerungCount,
		"scheduled_count":           stats.ScheduledCount,
		"verlaengerungsquote":       stats.Verlaengerungsquote,
		"umsatz_sum":                stats.UmsatzSum,
		"revenue_by_month":          revenueByMonth,
	})
}
