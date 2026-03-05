package api

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

func setCSVHeaders(w http.ResponseWriter, filename string) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
}

func parseMonthParam(v string) (time.Time, error) {
	if strings.TrimSpace(v) == "" {
		return time.Time{}, nil
	}
	return time.Parse("2006-01", v)
}

func monthStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func monthKey(t time.Time) string {
	return t.Format("2006-01")
}

func monthHeaderKey(t time.Time) string {
	return "m_" + t.Format("2006_01")
}

func parseDateString(v string) (time.Time, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}, false
	}
	formats := []string{"2006-01-02", "2006-01-02 15:04:05", time.RFC3339}
	for _, f := range formats {
		if t, err := time.Parse(f, v); err == nil {
			return t, true
		}
	}
	if len(v) >= 10 {
		if t, err := time.Parse("2006-01-02", v[:10]); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func buildMonthRangeInclusive(start, end time.Time) []time.Time {
	if end.Before(start) {
		return nil
	}
	out := make([]time.Time, 0, 24)
	cur := monthStart(start)
	last := monthStart(end)
	for !cur.After(last) {
		out = append(out, cur)
		cur = cur.AddDate(0, 1, 0)
	}
	return out
}

func asString(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func (h *Handler) ExportRawClientsCSV(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	rows, err := h.DB.QueryContext(ctx, `
		SELECT
			CAST(id AS TEXT),
			COALESCE(name, ''),
			COALESCE(email, ''),
			COALESCE(phone, ''),
			COALESCE(source, ''),
			COALESCE(CAST(source_stage_id AS TEXT), ''),
			COALESCE(status, ''),
			COALESCE(CAST(completed_at AS TEXT), ''),
			COALESCE(CAST(created_at AS TEXT), '')
		FROM clients
		ORDER BY id`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	setCSVHeaders(w, "clients.csv")
	cw := csv.NewWriter(w)
	defer cw.Flush()

	_ = cw.Write([]string{"id", "name", "email", "phone", "source", "source_stage_id", "status", "completed_at", "created_at"})

	for rows.Next() {
		var id, name, email, phone, source, sourceStageID, status, completedAt, createdAt string
		if err := rows.Scan(&id, &name, &email, &phone, &source, &sourceStageID, &status, &completedAt, &createdAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = cw.Write([]string{id, name, email, phone, source, sourceStageID, status, completedAt, createdAt})
	}
}

func (h *Handler) ExportRawContractsCSV(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	rows, err := h.DB.QueryContext(ctx, `
		SELECT
			CAST(id AS TEXT),
			COALESCE(CAST(client_id AS TEXT), ''),
			COALESCE(CAST(sales_process_id AS TEXT), ''),
			COALESCE(CAST(start_date AS TEXT), ''),
			COALESCE(CAST(end_date AS TEXT), ''),
			COALESCE(CAST(duration_months AS TEXT), ''),
			COALESCE(CAST(revenue_total AS TEXT), ''),
			COALESCE(payment_frequency, ''),
			COALESCE(CAST(created_at AS TEXT), ''),
			COALESCE(CAST(updated_at AS TEXT), '')
		FROM contracts
		ORDER BY id`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	setCSVHeaders(w, "contracts.csv")
	cw := csv.NewWriter(w)
	defer cw.Flush()

	_ = cw.Write([]string{"id", "client_id", "sales_process_id", "start_date", "end_date", "duration_months", "revenue_total", "payment_frequency", "created_at", "updated_at"})

	for rows.Next() {
		var id, clientID, salesProcessID, startDate, endDate, durationMonths, revenueTotal, paymentFrequency, createdAt, updatedAt string
		if err := rows.Scan(&id, &clientID, &salesProcessID, &startDate, &endDate, &durationMonths, &revenueTotal, &paymentFrequency, &createdAt, &updatedAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = cw.Write([]string{id, clientID, salesProcessID, startDate, endDate, durationMonths, revenueTotal, paymentFrequency, createdAt, updatedAt})
	}
}

func (h *Handler) ExportRawCashflowEntriesCSV(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	rows, err := h.DB.QueryContext(ctx, `
		SELECT
			CAST(id AS TEXT),
			COALESCE(CAST(contract_id AS TEXT), ''),
			COALESCE(CAST(due_date AS TEXT), ''),
			COALESCE(CAST(amount AS TEXT), ''),
			COALESCE(status, ''),
			COALESCE(CAST(updated_at AS TEXT), '')
		FROM cashflow_entries
		ORDER BY contract_id, due_date, id`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	setCSVHeaders(w, "cashflow_entries.csv")
	cw := csv.NewWriter(w)
	defer cw.Flush()

	_ = cw.Write([]string{"id", "contract_id", "due_date", "amount", "status", "updated_at"})

	for rows.Next() {
		var id, contractID, dueDate, amount, status, updatedAt string
		if err := rows.Scan(&id, &contractID, &dueDate, &amount, &status, &updatedAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = cw.Write([]string{id, contractID, dueDate, amount, status, updatedAt})
	}
}

type exportContractRow struct {
	ClientID       int
	ClientName     string
	ClientEmail    string
	ClientPhone    string
	ClientSource   string
	ClientStatus   string
	ContractID     int
	SalesProcessID string
	StartDate      time.Time
	EndDate        time.Time
	PaymentFreq    string
	DurationMonths int
	RevenueTotal   float64
	BaseAmount     string
}

func (h *Handler) ExportAggregatedCashflowCSV(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	fromParam := r.URL.Query().Get("from")
	toParam := r.URL.Query().Get("to")
	fromMonth, err := parseMonthParam(fromParam)
	if err != nil {
		http.Error(w, "invalid from (expected YYYY-MM)", http.StatusBadRequest)
		return
	}
	toMonth, err := parseMonthParam(toParam)
	if err != nil {
		http.Error(w, "invalid to (expected YYYY-MM)", http.StatusBadRequest)
		return
	}

	rows, err := h.DB.QueryContext(ctx, `
		SELECT
			c.id,
			COALESCE(cl.name, ''),
			COALESCE(cl.email, ''),
			COALESCE(cl.phone, ''),
			COALESCE(cl.source, ''),
			COALESCE(cl.status, ''),
			ct.id,
			COALESCE(CAST(ct.sales_process_id AS TEXT), ''),
			COALESCE(CAST(ct.start_date AS TEXT), ''),
			COALESCE(CAST(ct.end_date AS TEXT), ''),
			COALESCE(ct.payment_frequency, ''),
			COALESCE(ct.duration_months, 0),
			COALESCE(ct.revenue_total, 0)
		FROM contracts ct
		JOIN clients cl ON cl.id = ct.client_id
		JOIN clients c ON c.id = cl.id
		ORDER BY cl.id, ct.id`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	contracts := make([]exportContractRow, 0, 128)
	var minMonth time.Time
	var maxMonth time.Time

	for rows.Next() {
		var row exportContractRow
		var startDateRaw, endDateRaw string
		if err := rows.Scan(
			&row.ClientID,
			&row.ClientName,
			&row.ClientEmail,
			&row.ClientPhone,
			&row.ClientSource,
			&row.ClientStatus,
			&row.ContractID,
			&row.SalesProcessID,
			&startDateRaw,
			&endDateRaw,
			&row.PaymentFreq,
			&row.DurationMonths,
			&row.RevenueTotal,
		); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		st, ok := parseDateString(startDateRaw)
		if !ok {
			continue
		}
		row.StartDate = monthStart(st)

		if et, ok := parseDateString(endDateRaw); ok {
			row.EndDate = monthStart(et)
		} else {
			row.EndDate = monthStart(st.AddDate(0, row.DurationMonths, 0))
		}

		if row.DurationMonths > 0 {
			row.BaseAmount = strconv.FormatFloat(row.RevenueTotal/float64(row.DurationMonths), 'f', 2, 64)
		}

		if minMonth.IsZero() || row.StartDate.Before(minMonth) {
			minMonth = row.StartDate
		}
		if maxMonth.IsZero() || row.EndDate.After(maxMonth) {
			maxMonth = row.EndDate
		}

		contracts = append(contracts, row)
	}

	if len(contracts) == 0 {
		setCSVHeaders(w, "cashflow_aggregated.csv")
		cw := csv.NewWriter(w)
		defer cw.Flush()
		_ = cw.Write([]string{"client_id", "client_name", "client_email", "client_phone", "client_source", "client_status", "contract_id", "sales_process_id", "contract_start_date", "contract_end_date", "payment_frequency", "duration_months", "revenue_total", "base_amount"})
		return
	}

	if !fromMonth.IsZero() {
		minMonth = monthStart(fromMonth)
	}
	if !toMonth.IsZero() {
		maxMonth = monthStart(toMonth)
	}
	if maxMonth.Before(minMonth) {
		http.Error(w, "to must be >= from", http.StatusBadRequest)
		return
	}

	months := buildMonthRangeInclusive(minMonth, maxMonth)

	cashRows, err := h.DB.QueryContext(ctx, `
		SELECT
			contract_id,
			substr(CAST(due_date AS TEXT), 1, 7) AS ym,
			COALESCE(SUM(amount), 0)
		FROM cashflow_entries
		GROUP BY contract_id, substr(CAST(due_date AS TEXT), 1, 7)`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer cashRows.Close()

	amountByContractMonth := make(map[int]map[string]float64, 256)
	for cashRows.Next() {
		var contractID int
		var ym string
		var amount float64
		if err := cashRows.Scan(&contractID, &ym, &amount); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if _, ok := amountByContractMonth[contractID]; !ok {
			amountByContractMonth[contractID] = make(map[string]float64)
		}
		amountByContractMonth[contractID][ym] = amount
	}

	setCSVHeaders(w, "cashflow_aggregated.csv")
	cw := csv.NewWriter(w)
	defer cw.Flush()

	header := []string{"client_id", "client_name", "client_email", "client_phone", "client_source", "client_status", "contract_id", "sales_process_id", "contract_start_date", "contract_end_date", "payment_frequency", "duration_months", "revenue_total", "base_amount"}
	for _, m := range months {
		header = append(header, monthHeaderKey(m))
	}
	_ = cw.Write(header)

	sort.SliceStable(contracts, func(i, j int) bool {
		if contracts[i].ClientID == contracts[j].ClientID {
			return contracts[i].ContractID < contracts[j].ContractID
		}
		return contracts[i].ClientID < contracts[j].ClientID
	})

	for _, c := range contracts {
		row := []string{
			strconv.Itoa(c.ClientID),
			c.ClientName,
			c.ClientEmail,
			c.ClientPhone,
			c.ClientSource,
			c.ClientStatus,
			strconv.Itoa(c.ContractID),
			c.SalesProcessID,
			c.StartDate.Format("2006-01-02"),
			c.EndDate.Format("2006-01-02"),
			c.PaymentFreq,
			strconv.Itoa(c.DurationMonths),
			strconv.FormatFloat(c.RevenueTotal, 'f', 2, 64),
			c.BaseAmount,
		}

		contractMap := amountByContractMonth[c.ContractID]
		for _, m := range months {
			if m.Before(c.StartDate) || m.After(c.EndDate) {
				row = append(row, "")
				continue
			}
			amt := 0.0
			if contractMap != nil {
				if v, ok := contractMap[monthKey(m)]; ok {
					amt = v
				}
			}
			row = append(row, strconv.FormatFloat(amt, 'f', 2, 64))
		}

		_ = cw.Write(row)
	}
}
