package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/vr33ni-dev/annalanah-sales-assistant-go/pkg/mailer"

	"github.com/go-chi/chi/v5"
)

type Contract struct {
	ID             int                    `json:"id"`
	ClientID       int                    `json:"client_id"`
	SalesProcessID *int                   `json:"sales_process_id"`
	StartDate      string                 `json:"start_date"`
	CreatedAt      *string                `json:"created_at,omitempty"`
	EndDate        *string                `json:"end_date,omitempty"`
	DurationMonths int                    `json:"duration_months"`
	RevenueTotal   float64                `json:"revenue_total"`
	PaymentFreq    string                 `json:"payment_frequency"`
	Comments       []CommentCreateRequest `json:"comments,omitempty"`
}

type UpdateContractRequest struct {
	StartDate      string                 `json:"start_date"`
	DurationMonths int                    `json:"duration_months"`
	RevenueTotal   float64                `json:"revenue_total"`
	PaymentFreq    string                 `json:"payment_frequency"`
	Comments       []CommentCreateRequest `json:"comments,omitempty"`
}

type ContractResponse struct {
	ID                int                             `json:"id"`
	ClientID          int                             `json:"client_id"`
	ClientName        string                          `json:"client_name"`
	SalesProcessID    *int                            `json:"sales_process_id"`
	CreatedAt         *string                         `json:"created_at,omitempty"`
	UpdatedAt         *string                         `json:"updated_at,omitempty"`
	StartDate         string                          `json:"start_date"`
	EndDate           *string                         `json:"end_date,omitempty"`
	DurationMonths    int                             `json:"duration_months"`
	RevenueTotal      float64                         `json:"revenue_total"`
	PaymentFreq       string                          `json:"payment_frequency"`
	BaseMonthlyAmount float64                         `json:"base_monthly_amount"`
	NextDueDate       *string                         `json:"next_due_date,omitempty"`
	Source            string                          `json:"source"`
	Cashflow          []ContractCashflowEntryResponse `json:"cashflow,omitempty"`
	Comments          []CommentResponse               `json:"comments,omitempty"`
	Chain             []ContractResponse              `json:"chain,omitempty"`
}

type ContractCashflowEntryResponse struct {
	ID         int     `json:"id"`
	ContractID int     `json:"contract_id"`
	DueDate    *string `json:"due_date"`
	Amount     float64 `json:"amount"`
	Status     string  `json:"status"`
	UpdatedAt  *string `json:"updated_at,omitempty"`
}

func loadContractCashflowEntries(rows *sql.Rows) ([]ContractCashflowEntryResponse, error) {
	defer rows.Close()

	var out []ContractCashflowEntryResponse
	for rows.Next() {
		var e ContractCashflowEntryResponse
		var due sql.NullTime
		var updated sql.NullTime
		if err := rows.Scan(&e.ID, &e.ContractID, &due, &e.Amount, &e.Status, &updated); err != nil {
			return nil, err
		}
		if due.Valid {
			s := due.Time.Format(time.RFC3339)
			e.DueDate = &s
		}
		if updated.Valid {
			s := updated.Time.Format(time.RFC3339)
			e.UpdatedAt = &s
		}
		out = append(out, e)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	if out == nil {
		out = []ContractCashflowEntryResponse{}
	}

	return out, nil
}

type ContractCreateInput struct {
	ClientID          int
	SalesProcessID    *int
	StartDate         time.Time
	EndDate           *time.Time
	DurationMonths    int
	RevenueTotal      float64
	PaymentFreq       string
	CreatedAtOverride *time.Time
	GenerateSchedule  bool
	Source            string // "manual" or "imported"; defaults to "manual" if empty
}

func normalizePaymentFrequency(paymentFreq string, durationMonths int) (string, error) {
	pf := strings.ToLower(strings.TrimSpace(paymentFreq))
	if pf != "monthly" && pf != "bi-monthly" && pf != "quarterly" && pf != "one-time" && pf != "bi-yearly" {
		return "", fmt.Errorf("invalid payment_frequency (allowed: monthly, bi-monthly, quarterly, one-time, bi-yearly)")
	}
	if pf == "bi-yearly" && durationMonths < 12 {
		return "", fmt.Errorf("bi-yearly payment frequency requires duration_months >= 12")
	}
	return pf, nil
}

func (h *Handler) createContractTx(ctx context.Context, tx *sql.Tx, in ContractCreateInput) (int, *string, error) {
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
	INSERT INTO contracts
		(client_id, sales_process_id, start_date, end_date, duration_months, revenue_total, payment_frequency, source, created_at)
	VALUES ($1, $2, $3::date, $4::date, $5, $6, $7, $8, $9)
	ON CONFLICT (sales_process_id, start_date) WHERE sales_process_id IS NOT NULL
	DO NOTHING
	RETURNING id, created_at
	`,
			in.ClientID,
			in.SalesProcessID,
			in.StartDate,
			ed,
			in.DurationMonths,
			in.RevenueTotal,
			pf,
			source,
			*in.CreatedAtOverride,
		).Scan(&contractID, &createdAt)
	} else {
		err = tx.QueryRowContext(ctx, `
	INSERT INTO contracts
		(client_id, sales_process_id, start_date, end_date, duration_months, revenue_total, payment_frequency, source)
	VALUES ($1, $2, $3::date, $4::date, $5, $6, $7, $8)
	ON CONFLICT (sales_process_id, start_date) WHERE sales_process_id IS NOT NULL
	DO NOTHING
	RETURNING id, created_at
	`,
			in.ClientID,
			in.SalesProcessID,
			in.StartDate,
			ed,
			in.DurationMonths,
			in.RevenueTotal,
			pf,
			source,
		).Scan(&contractID, &createdAt)
	}

	if err == sql.ErrNoRows {
		// Duplicate contract (same sales_process_id + start_date already exists);
		// return the existing record so the caller gets a valid ID without creating a second row.
		var existingCreatedAt sql.NullTime
		if in.SalesProcessID != nil {
			err = tx.QueryRowContext(ctx,
				`SELECT id, created_at FROM contracts WHERE sales_process_id = $1 AND start_date = $2::date LIMIT 1`,
				in.SalesProcessID, in.StartDate,
			).Scan(&contractID, &existingCreatedAt)
			createdAt = existingCreatedAt
		}
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

func (h *Handler) notifyNewContractAsync(contractID, clientID int, revenue float64, startDate time.Time, salesProcessID *int) {
	if salesProcessID == nil {
		return
	}
	notifyTo := h.getTextSetting("new_contract_notify_email", "")
	if notifyTo == "" {
		notifyTo = os.Getenv("NEW_CONTRACT_NOTIFY_EMAIL")
	}
	if notifyTo == "" {
		return
	}

	clientName := fmt.Sprintf("Kunde #%d", clientID)
	closureDate := ""
	source := ""
	stageName := ""
	nextDueDate := ""

	var dbClientName sql.NullString
	var dbClosureDate sql.NullTime
	var dbSource sql.NullString
	var dbSourceStageName sql.NullString
	var dbSalesStageName sql.NullString
	var dbNextDueDate sql.NullTime

	err := h.DB.QueryRow(`
		SELECT
			cl.name,
			COALESCE(cl.completed_at::date, sp.follow_up_date::date) AS closure_date,
			cl.source,
			src_st.name AS source_stage_name,
			COALESCE(st.name, sp.stage) AS sales_stage_name,
			(
				SELECT MIN(due_date)::date
				FROM cashflow_entries
				WHERE contract_id = c.id AND status <> 'paid'
			) AS next_due_date
		FROM contracts c
		JOIN clients cl ON cl.id = c.client_id
		LEFT JOIN sales_process sp ON sp.id = c.sales_process_id
		LEFT JOIN stages st ON st.id = sp.stage_id
		LEFT JOIN stages src_st ON src_st.id = cl.source_stage_id
		WHERE c.id = $1
	`, contractID).Scan(&dbClientName, &dbClosureDate, &dbSource, &dbSourceStageName, &dbSalesStageName, &dbNextDueDate)

	if err == nil {
		if dbClientName.Valid && strings.TrimSpace(dbClientName.String) != "" {
			clientName = dbClientName.String
		}
		if dbClosureDate.Valid {
			closureDate = dbClosureDate.Time.Format("2006-01-02")
		}
		if dbSource.Valid {
			source = strings.TrimSpace(dbSource.String)
		}
		if dbSourceStageName.Valid && strings.TrimSpace(dbSourceStageName.String) != "" {
			stageName = strings.TrimSpace(dbSourceStageName.String)
		} else if dbSalesStageName.Valid {
			stageName = strings.TrimSpace(dbSalesStageName.String)
		}
		if dbNextDueDate.Valid {
			nextDueDate = dbNextDueDate.Time.Format("2006-01-02")
		}
	}

	go func() {
		if err := mailer.SendNewContractNotification(
			notifyTo,
			clientName,
			startDate.Format("2006-01-02"),
			closureDate,
			source,
			stageName,
			revenue,
			nextDueDate,
		); err != nil {
			fmt.Printf("failed to send new contract notification: %v\n", err)
		}
	}()
}

// GET /api/contracts
func (h *Handler) ListContracts(w http.ResponseWriter, r *http.Request) {
	includeExpired := strings.EqualFold(r.URL.Query().Get("include_expired"), "true")
	compact := strings.EqualFold(r.URL.Query().Get("compact"), "true")
	includeComments := !compact && !strings.EqualFold(r.URL.Query().Get("include_comments"), "false")
	includeCashflow := !strings.EqualFold(r.URL.Query().Get("include_cashflow"), "false")

	query := `

WITH RECURSIVE upsell_chain(terminal_id, node_id) AS (
	-- Anchor: contracts NOT replaced by any renewal (they are the active/terminal end of a chain)
	SELECT c.id, c.id
	FROM contracts c
	WHERE c.id NOT IN (
		SELECT previous_contract_id FROM contract_upsells
		WHERE previous_contract_id IS NOT NULL AND upsell_result = 'verlaengerung'
	)
	UNION ALL
	-- Walk backwards: find the predecessor of each node via upsell chain
	SELECT uc.terminal_id, cu.previous_contract_id
	FROM upsell_chain uc
	JOIN contract_upsells cu
		ON cu.new_contract_id = uc.node_id
		AND cu.upsell_result = 'verlaengerung'
		AND cu.previous_contract_id IS NOT NULL
),
chain_stats AS (
	SELECT
		uc.terminal_id,
		SUM(c2.revenue_total)   AS total_revenue,
		SUM(c2.duration_months) AS total_duration_months,
		MIN(c2.start_date)      AS chain_start_date
	FROM upsell_chain uc
	JOIN contracts c2 ON c2.id = uc.node_id
	GROUP BY uc.terminal_id
),
overdue AS (
	SELECT
		contract_id,
		MIN(due_date)::date AS overdue_due_date
	FROM cashflow_entries
	WHERE status = 'overdue'
	GROUP BY contract_id
),
upcoming AS (
	SELECT
		contract_id,
		MIN(due_date)::date AS upcoming_due_date
	FROM cashflow_entries
	WHERE due_date >= CURRENT_DATE
	GROUP BY contract_id
)
SELECT
	c.id,
	c.client_id,
	cl.name AS client_name,
	c.sales_process_id,
	cs.chain_start_date AS start_date,
	c.end_date,
	c.created_at,
	cs.total_duration_months AS duration_months,
	cs.total_revenue AS revenue_total,
	c.payment_frequency,
	CASE
		WHEN cs.total_duration_months > 0
			THEN (cs.total_revenue / cs.total_duration_months)
		ELSE 0
	END AS base_monthly_amount,
	COALESCE(
		o.overdue_due_date,
		u.upcoming_due_date
	) AS next_due_date,
	c.source
FROM contracts c
JOIN clients cl ON cl.id = c.client_id
JOIN chain_stats cs ON cs.terminal_id = c.id
LEFT JOIN overdue  o ON o.contract_id = c.id
LEFT JOIN upcoming u ON u.contract_id = c.id`

	if !includeExpired {
		query += `
WHERE (c.end_date IS NULL OR c.end_date >= CURRENT_DATE)
  AND cl.status = 'active'`
	} else {
		query += `
WHERE cl.status = 'inactive'
   OR c.end_date < CURRENT_DATE`
	}

	query += `
ORDER BY c.id;`

	rows, err := h.DB.Query(query)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	var out []ContractResponse
	var contractIDs []int
	idToIndex := make(map[int]int)

	for rows.Next() {
		var x ContractResponse
		if err := rows.Scan(
			&x.ID, &x.ClientID, &x.ClientName, &x.SalesProcessID,
			&x.StartDate, &x.EndDate, &x.CreatedAt, &x.DurationMonths, &x.RevenueTotal, &x.PaymentFreq,
			&x.BaseMonthlyAmount, &x.NextDueDate, &x.Source,
		); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		if includeComments {
			x.Comments = []CommentResponse{}
		}
		if includeCashflow {
			x.Cashflow = []ContractCashflowEntryResponse{}
		}

		if includeComments || includeCashflow {
			idToIndex[x.ID] = len(out)
			contractIDs = append(contractIDs, x.ID)
		}

		out = append(out, x)
	}

	// 🔥 Batch load contract comments (NO N+1)
	if includeComments && len(contractIDs) > 0 {
		commentRows, err := h.DB.Query(`
			SELECT id, entity_id, author, body, metadata, created_at, updated_at
			FROM comments
			WHERE entity_type = 'contract'
			  AND entity_id = ANY($1)
			ORDER BY created_at DESC
		`, pq.Array(contractIDs))

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
					&id, &entityID, &author, &body,
					&metadata, &created, &updated,
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
					out[idx].Comments = append(out[idx].Comments, CommentResponse{
						ID:         id,
						EntityType: "contract",
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

	if includeCashflow && len(contractIDs) > 0 {
		cashflowRows, err := h.DB.Query(`
			SELECT id, contract_id, due_date, amount, status, updated_at
			FROM cashflow_entries
			WHERE contract_id = ANY($1)
			ORDER BY contract_id, due_date, id
		`, pq.Array(contractIDs))
		if err == nil {
			entries, err := loadContractCashflowEntries(cashflowRows)
			if err == nil {
				for _, entry := range entries {
					if idx, ok := idToIndex[entry.ContractID]; ok {
						out[idx].Cashflow = append(out[idx].Cashflow, entry)
					}
				}
			}
		}
	}

	if out == nil {
		out = []ContractResponse{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// GET /api/contracts/{id}
func (h *Handler) GetContract(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid contract id", http.StatusBadRequest)
		return
	}

	var out ContractResponse
	var createdAt sql.NullTime
	var updatedAt sql.NullTime
	var endDate sql.NullTime
	var nextDueDate sql.NullTime

	err = h.DB.QueryRow(`
WITH overdue AS (
	SELECT
		contract_id,
		MIN(due_date)::date AS overdue_due_date
	FROM cashflow_entries
	WHERE status = 'overdue'
	GROUP BY contract_id
),
upcoming AS (
	SELECT
		contract_id,
		MIN(due_date)::date AS upcoming_due_date
	FROM cashflow_entries
	WHERE due_date >= CURRENT_DATE
	GROUP BY contract_id
)
SELECT
	c.id,
	c.client_id,
	cl.name AS client_name,
	c.sales_process_id,
	c.start_date,
	c.end_date,
	c.created_at,
	c.updated_at,
	c.duration_months,
	c.revenue_total,
	c.payment_frequency,
	CASE
		WHEN c.duration_months > 0
			THEN (c.revenue_total / c.duration_months)
		ELSE 0
	END AS base_monthly_amount,
	COALESCE(
		o.overdue_due_date,
		u.upcoming_due_date
	) AS next_due_date,
	c.source
FROM contracts c
JOIN clients cl ON cl.id = c.client_id
LEFT JOIN overdue  o ON o.contract_id = c.id
LEFT JOIN upcoming u ON u.contract_id = c.id
WHERE c.id = $1
`, id).Scan(
		&out.ID,
		&out.ClientID,
		&out.ClientName,
		&out.SalesProcessID,
		&out.StartDate,
		&endDate,
		&createdAt,
		&updatedAt,
		&out.DurationMonths,
		&out.RevenueTotal,
		&out.PaymentFreq,
		&out.BaseMonthlyAmount,
		&nextDueDate,
		&out.Source,
	)
	if err == sql.ErrNoRows {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if createdAt.Valid {
		s := createdAt.Time.Format(time.RFC3339)
		out.CreatedAt = &s
	}
	if updatedAt.Valid {
		s := updatedAt.Time.Format(time.RFC3339)
		out.UpdatedAt = &s
	}
	if endDate.Valid {
		s := endDate.Time.Format(time.RFC3339)
		out.EndDate = &s
	}
	if nextDueDate.Valid {
		s := nextDueDate.Time.Format(time.RFC3339)
		out.NextDueDate = &s
	}

	out.Comments = []CommentResponse{}
	out.Cashflow = []ContractCashflowEntryResponse{}

	// Populate the renewal chain (all contracts in the chain, ordered by start date)
	chainRows, err := h.DB.Query(`
WITH RECURSIVE
chain_root(contract_id) AS (
    SELECT $1::int
    UNION ALL
    SELECT cu.previous_contract_id
    FROM chain_root cr
    JOIN contract_upsells cu ON cu.new_contract_id = cr.contract_id
        AND cu.upsell_result = 'verlaengerung'
        AND cu.previous_contract_id IS NOT NULL
),
root AS (
    SELECT contract_id FROM chain_root ORDER BY contract_id ASC LIMIT 1
),
chain_forward(contract_id) AS (
    SELECT r.contract_id FROM root r
    UNION ALL
    SELECT cu.new_contract_id
    FROM chain_forward cf
    JOIN contract_upsells cu ON cu.previous_contract_id = cf.contract_id
        AND cu.upsell_result = 'verlaengerung'
        AND cu.new_contract_id IS NOT NULL
)
SELECT
    c.id, c.client_id, cl.name, c.sales_process_id,
    c.start_date, c.end_date, c.created_at, c.duration_months,
    c.revenue_total, c.payment_frequency,
    CASE WHEN c.duration_months > 0 THEN (c.revenue_total / c.duration_months) ELSE 0 END,
    COALESCE(o.overdue_due_date, u.upcoming_due_date),
    c.source
FROM chain_forward cf
JOIN contracts c ON c.id = cf.contract_id
JOIN clients cl ON cl.id = c.client_id
LEFT JOIN (
    SELECT contract_id, MIN(due_date)::date AS overdue_due_date
    FROM cashflow_entries WHERE status = 'overdue' GROUP BY contract_id
) o ON o.contract_id = c.id
LEFT JOIN (
    SELECT contract_id, MIN(due_date)::date AS upcoming_due_date
    FROM cashflow_entries WHERE due_date >= CURRENT_DATE GROUP BY contract_id
) u ON u.contract_id = c.id
ORDER BY c.start_date ASC, c.id ASC
`, id)
	if err == nil {
		defer chainRows.Close()
		for chainRows.Next() {
			var cx ContractResponse
			var cxEnd, cxCreated, cxNext sql.NullTime
			if err := chainRows.Scan(
				&cx.ID, &cx.ClientID, &cx.ClientName, &cx.SalesProcessID,
				&cx.StartDate, &cxEnd, &cxCreated, &cx.DurationMonths,
				&cx.RevenueTotal, &cx.PaymentFreq, &cx.BaseMonthlyAmount, &cxNext, &cx.Source,
			); err == nil {
				if cxEnd.Valid {
					s := cxEnd.Time.Format(time.RFC3339)
					cx.EndDate = &s
				}
				if cxCreated.Valid {
					s := cxCreated.Time.Format(time.RFC3339)
					cx.CreatedAt = &s
				}
				if cxNext.Valid {
					s := cxNext.Time.Format(time.RFC3339)
					cx.NextDueDate = &s
				}
				out.Chain = append(out.Chain, cx)
			}
		}
	}
	if out.Chain == nil {
		out.Chain = []ContractResponse{}
	}

	commentRows, err := h.DB.Query(`
		SELECT id, entity_id, author, body, metadata, created_at, updated_at
		FROM comments
		WHERE entity_type = 'contract'
		  AND entity_id = $1
		ORDER BY created_at DESC
	`, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer commentRows.Close()

	for commentRows.Next() {
		var commentID int
		var entityID int
		var author sql.NullString
		var body string
		var metadata sql.NullString
		var created, updated time.Time

		if err := commentRows.Scan(&commentID, &entityID, &author, &body, &metadata, &created, &updated); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
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

		out.Comments = append(out.Comments, CommentResponse{
			ID:         commentID,
			EntityType: "contract",
			EntityID:   entityID,
			Author:     a,
			Body:       body,
			Metadata:   meta,
			CreatedAt:  created.Format(time.RFC3339),
			UpdatedAt:  updated.Format(time.RFC3339),
		})
	}
	if err := commentRows.Err(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	cashflowRows, err := h.DB.Query(`
		SELECT id, contract_id, due_date, amount, status, updated_at
		FROM cashflow_entries
		WHERE contract_id = $1
		ORDER BY due_date, id
	`, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out.Cashflow, err = loadContractCashflowEntries(cashflowRows)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// GET /api/contracts/{id}/cashflow
func (h *Handler) ListContractCashflowEntries(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid contract id", http.StatusBadRequest)
		return
	}

	rows, err := h.DB.Query(`
		SELECT id, contract_id, due_date, amount, status, updated_at
		FROM cashflow_entries
		WHERE contract_id = $1
		ORDER BY due_date
	`, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out, err := loadContractCashflowEntries(rows)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// POST /api/contracts
func (h *Handler) CreateContract(w http.ResponseWriter, r *http.Request) {
	var c Contract

	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	pf, err := normalizePaymentFrequency(c.PaymentFreq, c.DurationMonths)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	c.PaymentFreq = pf

	// Parse start date
	sd, err := time.Parse("2006-01-02", c.StartDate)
	if err != nil {
		http.Error(w, "invalid start_date (expected YYYY-MM-DD)", http.StatusBadRequest)
		return
	}

	// Determine optional end date override
	var ed *time.Time
	if c.EndDate != nil && *c.EndDate != "" {
		parsedEnd, err := time.Parse("2006-01-02", *c.EndDate)
		if err != nil {
			http.Error(w, "invalid end_date (expected YYYY-MM-DD)", http.StatusBadRequest)
			return
		}
		if parsedEnd.Before(sd) {
			http.Error(w, "end_date cannot be before start_date", http.StatusBadRequest)
			return
		}
		ed = &parsedEnd
	}

	// Begin transaction
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	contractID, createdAt, err := h.createContractTx(r.Context(), tx, ContractCreateInput{
		ClientID:         c.ClientID,
		SalesProcessID:   c.SalesProcessID,
		StartDate:        sd,
		EndDate:          ed,
		DurationMonths:   c.DurationMonths,
		RevenueTotal:     c.RevenueTotal,
		PaymentFreq:      c.PaymentFreq,
		GenerateSchedule: true,
	})
	if err != nil {
		tx.Rollback()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	c.ID = contractID
	c.CreatedAt = createdAt

	// Commit transaction
	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Optionally insert comments (non-fatal)
	if len(c.Comments) > 0 {
		_ = h.insertCommentsForEntity("contract", c.ID, c.Comments)
	}

	// Return response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(c)

	// Send notification email only for sales-process contracts (not importer/manual without sales process)
	h.notifyNewContractAsync(c.ID, c.ClientID, c.RevenueTotal, sd, c.SalesProcessID)
}

// PATCH /api/contracts/{id}
func (h *Handler) UpdateContract(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid contract id", http.StatusBadRequest)
		return
	}

	var req UpdateContractRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Normalize and validate payment frequency
	pf := strings.ToLower(strings.TrimSpace(req.PaymentFreq))
	if pf != "monthly" && pf != "bi-monthly" && pf != "quarterly" && pf != "one-time" && pf != "bi-yearly" {
		http.Error(w, "invalid payment_frequency", http.StatusBadRequest)
		return
	}
	req.PaymentFreq = pf

	// Parse start date
	sd, err := time.Parse("2006-01-02", req.StartDate)
	if err != nil {
		http.Error(w, "invalid start_date (expected YYYY-MM-DD)", http.StatusBadRequest)
		return
	}

	// Compute end date explicitly
	ed := sd.AddDate(0, req.DurationMonths, 0)

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 1️⃣ Update contract fields
	_, err = tx.Exec(`
		UPDATE contracts
		SET 
			start_date = $1,
			end_date = $2,
			duration_months = $3,
			revenue_total = $4,
			payment_frequency = $5,
			updated_at = NOW()
		WHERE id = $6
	`,
		sd,
		ed,
		req.DurationMonths,
		req.RevenueTotal,
		req.PaymentFreq,
		id,
	)
	if err != nil {
		tx.Rollback()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 2️⃣ Delete ALL projection entries (derived data)
	_, err = tx.Exec(`
		DELETE FROM cashflow_entries
		WHERE contract_id = $1
	`, id)
	if err != nil {
		tx.Rollback()
		http.Error(w, "failed to clear projection schedule: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 3️⃣ Recreate projection schedule
	if err := insertCashflowEntriesTx(tx, id, sd, ed, req.RevenueTotal, req.PaymentFreq); err != nil {
		tx.Rollback()
		http.Error(w, "failed to regenerate schedule: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 4️⃣ Commit transaction
	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Optional comments (same behavior as before)
	if req.Comments != nil && len(req.Comments) > 0 {
		_ = h.insertCommentsForEntity("contract", id, req.Comments)
	}

	w.WriteHeader(http.StatusNoContent)
}
