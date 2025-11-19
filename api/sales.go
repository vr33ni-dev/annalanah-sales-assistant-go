package api

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

type SalesProcess struct {
	ID             int      `json:"id"`
	ClientID       int      `json:"client_id"`
	Stage          string   `json:"stage"`
	FollowUpDate   *string  `json:"follow_up_date"`
	FollowUpResult *bool    `json:"follow_up_result"`
	Closed         *bool    `json:"closed"`
	Revenue        *float64 `json:"revenue"`
	StageID        *int     `json:"stage_id"`
}

// What the API returns (GET /api/sales, PATCH /api/sales/{id})
type SalesProcessResponse struct {
	ID             int      `json:"id"`
	ClientID       int      `json:"client_id"`
	ClientName     string   `json:"client_name"`
	ClientEmail    *string  `json:"client_email,omitempty"`
	ClientPhone    *string  `json:"client_phone,omitempty"`
	ClientSource   *string  `json:"client_source,omitempty"`
	Stage          string   `json:"stage"`
	FollowUpDate   *string  `json:"follow_up_date"`
	FollowUpResult *bool    `json:"follow_up_result"`
	Closed         *bool    `json:"closed"`
	Revenue        *float64 `json:"revenue"`
	StageID        *int     `json:"stage_id"`
}

// What the API accepts (PATCH /api/sales/{id})
type SalesProcessUpdateRequest struct {
	FollowUpDate           *string  `json:"follow_up_date,omitempty"`
	FollowUpResult         *bool    `json:"follow_up_result"`
	Closed                 *bool    `json:"closed"`
	Revenue                *float64 `json:"revenue"`
	ContractDurationMonths *int     `json:"contract_duration_months,omitempty"`
	ContractStartDate      *string  `json:"contract_start_date,omitempty"`
	ContractFrequency      *string  `json:"contract_frequency,omitempty"`
	CompletedAt            *string  `json:"completed_at,omitempty"`
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
		sp.stage,
		sp.follow_up_date,
		sp.follow_up_result,
		sp.closed,
		CASE WHEN COALESCE(sp.closed, false) THEN sp.revenue ELSE NULL END AS revenue,
		sp.stage_id
	FROM sales_process sp
	JOIN clients cl ON cl.id = sp.client_id
	ORDER BY sp.created_at DESC, sp.id DESC
`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var processes []SalesProcessResponse
	for rows.Next() {
		var sp SalesProcessResponse
		if err := rows.Scan(
			&sp.ID,
			&sp.ClientID,
			&sp.ClientName,
			&sp.ClientEmail,
			&sp.ClientPhone,
			&sp.ClientSource,
			&sp.Stage,
			&sp.FollowUpDate,
			&sp.FollowUpResult,
			&sp.Closed,
			&sp.Revenue,
			&sp.StageID,
		); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		processes = append(processes, sp)
	}

	if processes == nil {
		processes = []SalesProcessResponse{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(processes)
}

// PATCH /api/sales/{id}
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

	// ---------- VALIDATION ----------
	// If closed=true, all contract fields must be present/valid.
	if sp.Closed != nil && *sp.Closed == true {
		if sp.Revenue == nil ||
			sp.ContractDurationMonths == nil || *sp.ContractDurationMonths <= 0 ||
			sp.ContractStartDate == nil ||
			sp.ContractFrequency == nil ||
			(*sp.ContractFrequency != "monthly" && *sp.ContractFrequency != "bi-monthly" && *sp.ContractFrequency != "quarterly") {
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
    follow_up_date   = COALESCE($1, follow_up_date),
    follow_up_result = COALESCE($2, follow_up_result),
    closed           = COALESCE($3, closed),
    revenue          = CASE
      WHEN $3 IS TRUE  THEN $4
      WHEN $3 IS FALSE THEN NULL
      ELSE revenue
    END,
    stage = CASE
      WHEN COALESCE($3, closed) IS TRUE  THEN 'closed'
      WHEN COALESCE($3, closed) IS FALSE THEN 'lost'
      WHEN COALESCE($2, follow_up_result) IS FALSE THEN 'lost'
      WHEN COALESCE($2, follow_up_result) IS TRUE  THEN 'follow_up'
      ELSE 'follow_up'
    END
  WHERE id = $5
`,
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

	// ---------- SYNC CLIENT STATUS ----------
	_, err = h.DB.Exec(`
	  WITH s AS (
	    SELECT client_id, stage, follow_up_result, closed
	    FROM sales_process WHERE id = $1
	  )
	  UPDATE clients c
	  SET status = CASE
	    WHEN (SELECT stage FROM s) = 'closed'
	         AND COALESCE((SELECT closed FROM s), FALSE) = TRUE
	      THEN 'active'
	    WHEN (SELECT stage FROM s) = 'lost'
	      THEN 'lost'
	    WHEN (SELECT stage FROM s) = 'follow_up'
	         AND (SELECT follow_up_result FROM s) IS NULL
	      THEN 'follow_up_scheduled'
	    WHEN (SELECT stage FROM s) = 'follow_up'
	         AND (SELECT follow_up_result FROM s) IS TRUE
	      THEN 'awaiting_response'
	    ELSE c.status
	  END
	  WHERE c.id = (SELECT client_id FROM s)
	`, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// ---------- (OPTIONAL) AUTO-CREATE CONTRACT ON CLOSE-WON ----------
	if sp.Closed != nil && *sp.Closed == true &&
		sp.Revenue != nil &&
		sp.ContractDurationMonths != nil && *sp.ContractDurationMonths > 0 &&
		sp.ContractStartDate != nil && sp.ContractFrequency != nil {

		// get client_id for this sales process
		var clientID int
		if err := h.DB.QueryRow(`SELECT client_id FROM sales_process WHERE id = $1`, id).Scan(&clientID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// If Closed is explicitly true, update completed_at to now
		// Set completed_at to selected date if closed is true and completed_at is provided
		if sp.Closed != nil && *sp.Closed && sp.CompletedAt != nil {
			_, err := h.DB.Exec(`
		UPDATE clients
		SET completed_at = $1::date
		WHERE id = $2
	`, *sp.CompletedAt, clientID)
			if err != nil {
				http.Error(w, "failed to update completed_at: "+err.Error(), http.StatusInternalServerError)
				return
			}
		} else if sp.Closed != nil && !*sp.Closed {
			_, err := h.DB.Exec(`UPDATE clients SET completed_at = NULL WHERE id = $1`, clientID)
			if err != nil {
				http.Error(w, "failed to clear completed_at: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}

		// avoid duplicate active contract
		var exists bool
		if err := h.DB.QueryRow(`
			SELECT EXISTS (SELECT 1 FROM contracts WHERE client_id = $1 AND end_date_computed IS NULL)
		`, clientID).Scan(&exists); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if !exists {
			_, err = h.DB.Exec(`
				INSERT INTO contracts
					(client_id, sales_process_id, start_date, duration_months, revenue_total, payment_frequency)
				VALUES ($1, $2, $3::date, $4, $5, $6)
			`, clientID, id, *sp.ContractStartDate, *sp.ContractDurationMonths, *sp.Revenue, *sp.ContractFrequency)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
	}

	// ---------- RETURN UPDATED ROW ----------
	row := h.DB.QueryRow(`
	  SELECT
	    sp.id,
	    sp.client_id,
	    c.name  AS client_name,
	    c.email AS client_email,
	    c.phone AS client_phone,
	    c.source AS client_source,
	    sp.stage,
	    sp.follow_up_date,
	    sp.follow_up_result,
	    sp.closed,
	    CASE WHEN COALESCE(sp.closed, false) THEN sp.revenue ELSE NULL END AS revenue,
	    sp.stage_id
	  FROM sales_process sp
	  JOIN clients c ON c.id = sp.client_id
	  WHERE sp.id = $1
	`, id)

	var updated SalesProcessResponse
	if err := row.Scan(
		&updated.ID,
		&updated.ClientID,
		&updated.ClientName,
		&updated.ClientEmail,
		&updated.ClientPhone,
		&updated.ClientSource,
		&updated.Stage,
		&updated.FollowUpDate,
		&updated.FollowUpResult,
		&updated.Closed,
		&updated.Revenue,
		&updated.StageID,
	); err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "sales process not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(updated)
}

// POST /api/sales/start
// types you already have somewhere are fine; these keep the payload stable.
type StartSalesProcessRequest struct {
	Name          string  `json:"name"`
	Email         string  `json:"email"`
	Phone         string  `json:"phone"`
	Source        string  `json:"source"` // "organic" | "paid"
	SourceStageID *int    `json:"source_stage_id,omitempty"`
	FollowUpDate  *string `json:"follow_up_date"`
}

type StartSalesProcessClient struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	Email         string `json:"email"`
	Phone         string `json:"phone"`
	Source        string `json:"source"`
	SourceStageID *int   `json:"source_stage_id,omitempty"`
}

type StartSalesProcessDTO struct {
	ID             int     `json:"id"`
	ClientID       int     `json:"client_id"`
	Stage          string  `json:"stage"`
	FollowUpDate   *string `json:"follow_up_date"`
	FollowUpResult *bool   `json:"follow_up_result"`
	Closed         *bool   `json:"closed"`
	Revenue        *int    `json:"revenue"`
	StageID        *int    `json:"stage_id"`
}

type StartSalesProcessResponse struct {
	SalesProcessID int                     `json:"sales_process_id"`
	Client         StartSalesProcessClient `json:"client"`
	SalesProcess   StartSalesProcessDTO    `json:"sales_process"`
}

func (h *Handler) StartSalesProcess(w http.ResponseWriter, r *http.Request) {
	var req StartSalesProcessRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.FollowUpDate == nil || *req.FollowUpDate == "" {
		http.Error(w, "follow_up_date is required", http.StatusBadRequest)
		return
	}

	tx, err := h.DB.Begin()
	if err != nil {
		http.Error(w, "begin tx: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	// 1) insert client
	var clientID int
	if err := tx.QueryRow(
		`INSERT INTO clients (name, email, phone, source, source_stage_id, status)
	 VALUES ($1, $2, $3, $4, $5, 'follow_up_scheduled')
	 RETURNING id`,
		req.Name, req.Email, req.Phone, req.Source, req.SourceStageID,
	).Scan(&clientID); err != nil {

		if isUniqueViolation(err, "unique_client_email") {
			http.Error(w, "Ein Kunde mit dieser E-Mail-Adresse existiert bereits.", http.StatusConflict)
			return
		}

		http.Error(w, "insert client: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 2) insert sales process
	var salesProcessID int
	if err := tx.QueryRow(
		`INSERT INTO sales_process (client_id, stage, follow_up_date, stage_id)
		 VALUES ($1, 'follow_up', $2, $3)
		 RETURNING id`,
		clientID, req.FollowUpDate, req.SourceStageID,
	).Scan(&salesProcessID); err != nil {
		http.Error(w, "insert sales_process: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, "commit: "+err.Error(), http.StatusInternalServerError)
		return
	}

	resp := StartSalesProcessResponse{
		SalesProcessID: salesProcessID,
		Client: StartSalesProcessClient{
			ID:            clientID,
			Name:          req.Name,
			Email:         req.Email,
			Phone:         req.Phone,
			Source:        req.Source,
			SourceStageID: req.SourceStageID,
		},
		SalesProcess: StartSalesProcessDTO{
			ID:             salesProcessID,
			ClientID:       clientID,
			Stage:          "follow_up",
			FollowUpDate:   req.FollowUpDate,
			FollowUpResult: nil,
			Closed:         nil,
			Revenue:        nil,
			StageID:        req.SourceStageID,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated) // 201 + body
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		// last-ditch error path: headers are already sent; just log
		log.Printf("encode StartSalesProcessResponse failed: %v", err)
	}
}

// Upsells
// Using Pointers because PostgreSQL can return NULL, and Go's plain string cannot represent NULL, only "" and these fields can return nil
type ContractUpsell struct {
	ID                 int      `json:"id"`
	SalesProcessID     int      `json:"sales_process_id"`
	ClientID           int      `json:"client_id"`
	UpsellDate         *string  `json:"upsell_date"`
	UpsellResult       *string  `json:"upsell_result"` // "verlaengerung" or "keine_verlaengerung"`
	UpsellRevenue      *float64 `json:"upsell_revenue,omitempty"`
	PreviousContractID *int     `json:"previous_contract_id,omitempty"`
	NewContractID      *int     `json:"new_contract_id,omitempty"`
}
type CreateUpsellRequest struct {
	UpsellDate    *string  `json:"upsell_date,omitempty"`
	UpsellResult  *string  `json:"upsell_result,omitempty"`
	UpsellRevenue *float64 `json:"upsell_revenue,omitempty"`
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
            id,
            sales_process_id,
            client_id,
            upsell_date,
            upsell_result,
            upsell_revenue,
            previous_contract_id,
            new_contract_id
        FROM contract_upsells
        WHERE sales_process_id = $1
        ORDER BY upsell_date DESC, id DESC
    `, salesID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	var list []ContractUpsell

	for rows.Next() {
		var u ContractUpsell
		if err := rows.Scan(
			&u.ID,
			&u.SalesProcessID,
			&u.ClientID,
			&u.UpsellDate,
			&u.UpsellResult,
			&u.UpsellRevenue,
			&u.PreviousContractID,
			&u.NewContractID,
		); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		list = append(list, u)
	}

	if list == nil {
		list = []ContractUpsell{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

func (h *Handler) ListUpsellCategories(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.Query(`
        SELECT 
            id,
            sales_process_id,
            client_id,
            upsell_date,
            upsell_result,
            upsell_revenue,
            previous_contract_id,
            new_contract_id
        FROM contract_upsells
        ORDER BY upsell_date DESC NULLS LAST, id DESC
    `)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	// categories
	var scheduled []ContractUpsell
	var successful []ContractUpsell
	var unsuccessful []ContractUpsell

	for rows.Next() {
		var u ContractUpsell

		err := rows.Scan(
			&u.ID,
			&u.SalesProcessID,
			&u.ClientID,
			&u.UpsellDate,
			&u.UpsellResult,
			&u.UpsellRevenue,
			&u.PreviousContractID,
			&u.NewContractID,
		)
		if err != nil {
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

	// return structured response
	resp := map[string]any{
		"scheduled":    scheduled,
		"successful":   successful,
		"unsuccessful": unsuccessful,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
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
        WHERE sales_process_id = $1 AND upsell_result IS NULL
        ORDER BY upsell_date DESC LIMIT 1
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
        WHERE client_id = $1 AND end_date_computed IS NULL
        ORDER BY id DESC LIMIT 1
    `, clientID).Scan(&prevContractID)

	// -----------------------
	// If verlängerung → create new contract + cashflow
	// -----------------------

	var newContractID *int = nil

	if req.UpsellResult != nil && *req.UpsellResult == "verlaengerung" {
		err = tx.QueryRow(`
            INSERT INTO contracts (client_id, sales_process_id, start_date,
                                   duration_months, revenue_total, payment_frequency)
            VALUES ($1, $2, CURRENT_DATE, 6, $3, 'monthly')
            RETURNING id
        `, clientID, salesID, *req.UpsellRevenue).Scan(&newContractID)
		if err != nil {
			http.Error(w, "failed to create contract: "+err.Error(), 500)
			return
		}

		_, err = tx.Exec(`
            INSERT INTO cashflow_entries (contract_id, due_date, amount, status)
            VALUES ($1, CURRENT_DATE + INTERVAL '30 days', $2, 'pending')
        `, *newContractID, *req.UpsellRevenue/6)
		if err != nil {
			http.Error(w, "failed to create cashflow entries: "+err.Error(), 500)
			return
		}
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
			req.UpsellDate,    // may be NULL
			req.UpsellResult,  // may be NULL
			req.UpsellRevenue, // may be NULL
			prevContractID,
			newContractID,
		).Scan(&upsellID)
	} else {
		// UPDATE
		err = tx.QueryRow(`
            UPDATE contract_upsells
            SET
                upsell_date   = COALESCE($2, upsell_date),
                upsell_result = COALESCE($3, upsell_result),
                upsell_revenue = COALESCE($4, upsell_revenue),
                new_contract_id = COALESCE($5, new_contract_id)
            WHERE id = $1
            RETURNING id
        `,
			*existingUpsellID,
			req.UpsellDate,
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"upsell_id":       upsellID,
		"updated":         existingUpsellID != nil,
		"new_contract_id": newContractID,
	})
}

func (h *Handler) GetUpsellAnalytics(w http.ResponseWriter, r *http.Request) {

	var stats struct {
		VerlaengerungCount      int      `json:"verlangerung_count"`
		KeineVerlaengerungCount int      `json:"keine_verlangerung_count"`
		ScheduledCount          int      `json:"scheduled_count"`
		Verlaengerungsquote     *float64 `json:"verlangerungsquote"`
		UmsatzSum               float64  `json:"umsatz_sum"`
	}

	err := h.DB.QueryRow(`
        SELECT
			COUNT(*) FILTER (WHERE upsell_result = 'verlaengerung')         AS verlangerung_count,
			COUNT(*) FILTER (WHERE upsell_result = 'keine_verlaengerung')  AS keine_verlangerung_count,
			COUNT(*) FILTER (WHERE upsell_result IS NULL)                  AS scheduled_count,

			ROUND(
				100.0 * COUNT(*) FILTER (WHERE upsell_result = 'verlaengerung')
				/ NULLIF(
					COUNT(*) FILTER (WHERE upsell_result IN ('verlaengerung','keine_verlaengerung')),
					0
				),
				1
			) AS verlangerungsquote,

			COALESCE(SUM(upsell_revenue), 0) AS umsatz_sum
        FROM contract_upsells;
    `).Scan(
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}
