package api

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
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
			COALESCE((SELECT name FROM stages s WHERE s.id = clients.source_stage_id), ''),
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

	_ = cw.Write([]string{"id", "name", "email", "phone", "source", "source_stage_name", "source_stage_id", "status", "completed_at", "created_at"})

	for rows.Next() {
		var id, name, email, phone, source, sourceStageName, sourceStageID, status, completedAt, createdAt string
		if err := rows.Scan(&id, &name, &email, &phone, &source, &sourceStageName, &sourceStageID, &status, &completedAt, &createdAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = cw.Write([]string{id, name, email, phone, source, sourceStageName, sourceStageID, status, completedAt, createdAt})
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
			COALESCE(CAST(ROUND(CAST(revenue_total AS NUMERIC), 2) AS TEXT), ''),
			COALESCE(payment_frequency, ''),
			COALESCE(source, ''),
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

	_ = cw.Write([]string{"id", "client_id", "sales_process_id", "start_date", "end_date", "duration_months", "revenue_total", "payment_frequency", "source", "created_at", "updated_at"})

	for rows.Next() {
		var id, clientID, salesProcessID, startDate, endDate, durationMonths, revenueTotal, paymentFrequency, source, createdAt, updatedAt string
		if err := rows.Scan(&id, &clientID, &salesProcessID, &startDate, &endDate, &durationMonths, &revenueTotal, &paymentFrequency, &source, &createdAt, &updatedAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = cw.Write([]string{id, clientID, salesProcessID, startDate, endDate, durationMonths, revenueTotal, paymentFrequency, source, createdAt, updatedAt})
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
			COALESCE(CAST(ROUND(CAST(amount AS NUMERIC), 2) AS TEXT), ''),
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

var germanMonthNames = [12]string{
	"Januar", "Februar", "März", "April", "Mai", "Juni",
	"Juli", "August", "September", "Oktober", "November", "Dezember",
}

func germanMonthHeader(t time.Time) string {
	return germanMonthNames[t.Month()-1] + " '" + t.Format("06")
}

// splitClientName splits a full name into (nachname, vorname) by taking the
// last space-separated word as the family name.
func splitClientName(name string) (nachname, vorname string) {
	idx := strings.LastIndex(name, " ")
	if idx < 0 {
		return name, ""
	}
	return name[idx+1:], name[:idx]
}

// ExportLegacyCashflowCSV produces one row per client with German month headers,
// CLV, and actual cashflow amounts summed across all contracts.
func (h *Handler) ExportLegacyCashflowCSV(w http.ResponseWriter, r *http.Request) {
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

	type clientRow struct {
		ID              int
		Name            string
		Status          string
		StartDate       string
		EndDate         string
		CLV             float64
		Source          string
		SourceStageName string
	}

	rows, err := h.DB.QueryContext(ctx, `
		SELECT
			cl.id,
			COALESCE(cl.name, ''),
			COALESCE(cl.status, ''),
			COALESCE(CAST(MIN(ct.start_date) AS TEXT), ''),
			COALESCE(CAST(MAX(ct.end_date) AS TEXT), ''),
			COALESCE(SUM(ct.revenue_total), 0),
			COALESCE(cl.source, ''),
			COALESCE((SELECT name FROM stages s WHERE s.id = cl.source_stage_id), '')
		FROM clients cl
		JOIN contracts ct ON ct.client_id = cl.id
		GROUP BY cl.id, cl.name, cl.status, cl.source, cl.source_stage_id
		ORDER BY cl.id`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	clients := make([]clientRow, 0, 128)
	var minData, maxData time.Time
	for rows.Next() {
		var c clientRow
		if err := rows.Scan(&c.ID, &c.Name, &c.Status, &c.StartDate, &c.EndDate, &c.CLV, &c.Source, &c.SourceStageName); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		clients = append(clients, c)
		if st, ok := parseDateString(c.StartDate); ok {
			ms := monthStart(st)
			if minData.IsZero() || ms.Before(minData) {
				minData = ms
			}
		}
		if et, ok := parseDateString(c.EndDate); ok {
			ms := monthStart(et)
			if maxData.IsZero() || ms.After(maxData) {
				maxData = ms
			}
		}
	}

	setCSVHeaders(w, "cashflow_aggregated.csv")
	cw := csv.NewWriter(w)
	defer cw.Flush()

	if len(clients) == 0 {
		_ = cw.Write([]string{"status", "nachname", "vorname", "startzeitpunkt", "endzeitpunkt", "clv", "quelle", "quelle_stage", "upsells", "kommentare"})
		return
	}

	if !fromMonth.IsZero() {
		minData = monthStart(fromMonth)
	}
	if !toMonth.IsZero() {
		maxData = monthStart(toMonth)
	}
	if maxData.Before(minData) {
		http.Error(w, "to must be >= from", http.StatusBadRequest)
		return
	}

	months := buildMonthRangeInclusive(minData, maxData)

	// Load cashflow entries aggregated per client per month.
	cashRows, err := h.DB.QueryContext(ctx, `
		SELECT
			ct.client_id,
			substr(CAST(ce.due_date AS TEXT), 1, 7) AS ym,
			SUM(ce.amount)
		FROM cashflow_entries ce
		JOIN contracts ct ON ct.id = ce.contract_id
		GROUP BY ct.client_id, substr(CAST(ce.due_date AS TEXT), 1, 7)`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer cashRows.Close()

	amountByClientMonth := make(map[int]map[string]float64)
	for cashRows.Next() {
		var clientID int
		var ym string
		var amount float64
		if err := cashRows.Scan(&clientID, &ym, &amount); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if amountByClientMonth[clientID] == nil {
			amountByClientMonth[clientID] = make(map[string]float64)
		}
		amountByClientMonth[clientID][ym] = amount
	}

	// Load upsell results per client (ordered by date).
	upsellRows, err := h.DB.QueryContext(ctx, `
		SELECT client_id, upsell_result
		FROM contract_upsells
		ORDER BY client_id, upsell_date`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer upsellRows.Close()

	upsellsByClient := make(map[int]map[string]int)
	for upsellRows.Next() {
		var clientID int
		var result string
		if err := upsellRows.Scan(&clientID, &result); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if upsellsByClient[clientID] == nil {
			upsellsByClient[clientID] = make(map[string]int)
		}
		upsellsByClient[clientID][result]++
	}

	// Load comments for entity_type='client', ordered oldest first.
	commentRows, err := h.DB.QueryContext(ctx, `
		SELECT entity_id, body
		FROM comments
		WHERE entity_type = 'client'
		ORDER BY entity_id, created_at`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer commentRows.Close()

	commentsByClient := make(map[int][]string)
	for commentRows.Next() {
		var clientID int
		var body string
		if err := commentRows.Scan(&clientID, &body); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		commentsByClient[clientID] = append(commentsByClient[clientID], body)
	}

	header := []string{"status", "nachname", "vorname", "startzeitpunkt", "endzeitpunkt", "clv", "quelle", "quelle_stage", "upsells", "kommentare"}
	for _, m := range months {
		header = append(header, germanMonthHeader(m))
	}
	_ = cw.Write(header)

	for _, c := range clients {
		nachname, vorname := splitClientName(c.Name)
		clvStr := strconv.FormatFloat(c.CLV, 'f', 2, 64)
		// Format dates as DD.MM.YYYY if parseable
		startFmt, endFmt := c.StartDate, c.EndDate
		if t, ok := parseDateString(c.StartDate); ok {
			startFmt = t.Format("02.01.2006")
		}
		if t, ok := parseDateString(c.EndDate); ok {
			endFmt = t.Format("02.01.2006")
		}
		upsells := "keine verlängerung"
		if n, ok := upsellsByClient[c.ID]["verlaengerung"]; ok {
			upsells = fmt.Sprintf("%dx verlängerung", n)
		}
		kommentare := strings.Join(commentsByClient[c.ID], ", ")
		row := []string{c.Status, nachname, vorname, startFmt, endFmt, clvStr, c.Source, c.SourceStageName, upsells, kommentare}
		clientMap := amountByClientMonth[c.ID]
		for _, m := range months {
			ym := monthKey(m)
			if v, ok := clientMap[ym]; ok {
				row = append(row, strconv.FormatFloat(v, 'f', 2, 64))
			} else {
				row = append(row, "")
			}
		}
		_ = cw.Write(row)
	}
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

	type aggClientRow struct {
		ID              int
		Name            string
		Email           string
		Phone           string
		Source          string
		SourceStageName string
		Status          string
		StartDate       string
		EndDate         string
		TotalRevenue    float64
	}

	rows, err := h.DB.QueryContext(ctx, `
		SELECT
			cl.id,
			COALESCE(cl.name, ''),
			COALESCE(cl.email, ''),
			COALESCE(cl.phone, ''),
			COALESCE(cl.source, ''),
			COALESCE((SELECT name FROM stages s WHERE s.id = cl.source_stage_id), ''),
			COALESCE(cl.status, ''),
			COALESCE(CAST(MIN(ct.start_date) AS TEXT), ''),
			COALESCE(CAST(MAX(ct.end_date) AS TEXT), ''),
			COALESCE(SUM(ct.revenue_total), 0)
		FROM clients cl
		JOIN contracts ct ON ct.client_id = cl.id
		GROUP BY cl.id, cl.name, cl.email, cl.phone, cl.source, cl.source_stage_id, cl.status
		ORDER BY cl.id`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	clients := make([]aggClientRow, 0, 128)
	var minMonth, maxMonth time.Time

	for rows.Next() {
		var c aggClientRow
		if err := rows.Scan(&c.ID, &c.Name, &c.Email, &c.Phone, &c.Source, &c.SourceStageName, &c.Status, &c.StartDate, &c.EndDate, &c.TotalRevenue); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		clients = append(clients, c)
		if st, ok := parseDateString(c.StartDate); ok {
			ms := monthStart(st)
			if minMonth.IsZero() || ms.Before(minMonth) {
				minMonth = ms
			}
		}
		if et, ok := parseDateString(c.EndDate); ok {
			ms := monthStart(et)
			if maxMonth.IsZero() || ms.After(maxMonth) {
				maxMonth = ms
			}
		}
	}

	setCSVHeaders(w, "cashflow_aggregated.csv")
	cw := csv.NewWriter(w)
	defer cw.Flush()

	if len(clients) == 0 {
		_ = cw.Write([]string{"client_id", "client_name", "client_email", "client_phone", "client_source", "client_source_stage_name", "client_status", "first_contract_start", "last_contract_end", "total_revenue"})
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
			ct.client_id,
			substr(CAST(ce.due_date AS TEXT), 1, 7) AS ym,
			SUM(ce.amount)
		FROM cashflow_entries ce
		JOIN contracts ct ON ct.id = ce.contract_id
		GROUP BY ct.client_id, substr(CAST(ce.due_date AS TEXT), 1, 7)`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer cashRows.Close()

	amountByClientMonth := make(map[int]map[string]float64)
	for cashRows.Next() {
		var clientID int
		var ym string
		var amount float64
		if err := cashRows.Scan(&clientID, &ym, &amount); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if amountByClientMonth[clientID] == nil {
			amountByClientMonth[clientID] = make(map[string]float64)
		}
		amountByClientMonth[clientID][ym] = amount
	}

	header := []string{"client_id", "client_name", "client_email", "client_phone", "client_source", "client_source_stage_name", "client_status", "first_contract_start", "last_contract_end", "total_revenue"}
	for _, m := range months {
		header = append(header, monthHeaderKey(m))
	}
	_ = cw.Write(header)

	for _, c := range clients {
		startFmt, endFmt := c.StartDate, c.EndDate
		if t, ok := parseDateString(c.StartDate); ok {
			startFmt = t.Format("2006-01-02")
		}
		if t, ok := parseDateString(c.EndDate); ok {
			endFmt = t.Format("2006-01-02")
		}
		row := []string{
			strconv.Itoa(c.ID),
			c.Name,
			c.Email,
			c.Phone,
			c.Source,
			c.SourceStageName,
			c.Status,
			startFmt,
			endFmt,
			strconv.FormatFloat(c.TotalRevenue, 'f', 2, 64),
		}
		clientMap := amountByClientMonth[c.ID]
		for _, m := range months {
			ym := monthKey(m)
			if v, ok := clientMap[ym]; ok {
				row = append(row, strconv.FormatFloat(v, 'f', 2, 64))
			} else {
				row = append(row, "0.00")
			}
		}
		_ = cw.Write(row)
	}
}
