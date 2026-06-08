// exports.go — HTTP handlers for CSV exports: raw clients, contracts, cashflow entries, legacy and aggregated cashflow.
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

	rows, err := h.store.ExportClientsRaw(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	setCSVHeaders(w, "clients.csv")
	cw := csv.NewWriter(w)
	defer cw.Flush()

	_ = cw.Write([]string{"id", "name", "email", "phone", "source", "source_stage_name", "source_stage_id", "status", "completed_at", "created_at"})
	for _, row := range rows {
		_ = cw.Write(row)
	}
}

// GET /api/exports/raw/contracts.csv
func (h *Handler) ExportRawContractsCSV(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	rows, err := h.store.ExportContractsRaw(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	setCSVHeaders(w, "contracts.csv")
	cw := csv.NewWriter(w)
	defer cw.Flush()

	_ = cw.Write([]string{"id", "client_id", "sales_process_id", "start_date", "end_date", "duration_months", "revenue_total", "payment_frequency", "source", "created_at", "updated_at"})
	for _, row := range rows {
		_ = cw.Write(row)
	}
}

// GET /api/exports/raw/cashflow_entries.csv
func (h *Handler) ExportRawCashflowEntriesCSV(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	rows, err := h.store.ExportCashflowEntriesRaw(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	setCSVHeaders(w, "cashflow_entries.csv")
	cw := csv.NewWriter(w)
	defer cw.Flush()

	_ = cw.Write([]string{"id", "contract_id", "due_date", "amount", "status", "updated_at"})
	for _, row := range rows {
		_ = cw.Write(row)
	}
}

var germanMonthNames = [12]string{
	"Januar", "Februar", "März", "April", "Mai", "Juni",
	"Juli", "August", "September", "Oktober", "November", "Dezember",
}

func germanMonthHeader(t time.Time) string {
	return germanMonthNames[t.Month()-1] + " '" + t.Format("06")
}

func splitClientName(name string) (nachname, vorname string) {
	idx := strings.LastIndex(name, " ")
	if idx < 0 {
		return name, ""
	}
	return name[idx+1:], name[:idx]
}

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

	data, err := h.store.ExportLegacyCashflow(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	setCSVHeaders(w, "cashflow_aggregated.csv")
	cw := csv.NewWriter(w)
	defer cw.Flush()

	if len(data.Clients) == 0 {
		_ = cw.Write([]string{"status", "nachname", "vorname", "startzeitpunkt", "endzeitpunkt", "clv", "quelle", "quelle_stage", "upsells", "kommentare"})
		return
	}

	var minData, maxData time.Time
	for _, c := range data.Clients {
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

	header := []string{"status", "nachname", "vorname", "startzeitpunkt", "endzeitpunkt", "clv", "quelle", "quelle_stage", "upsells", "kommentare"}
	for _, m := range months {
		header = append(header, germanMonthHeader(m))
	}
	_ = cw.Write(header)

	for _, c := range data.Clients {
		nachname, vorname := splitClientName(c.Name)
		clvStr := strconv.FormatFloat(c.CLV, 'f', 2, 64)
		startFmt, endFmt := c.StartDate, c.EndDate
		if t, ok := parseDateString(c.StartDate); ok {
			startFmt = t.Format("02.01.2006")
		}
		if t, ok := parseDateString(c.EndDate); ok {
			endFmt = t.Format("02.01.2006")
		}
		upsells := "keine verlängerung"
		if n, ok := data.UpsellsByClient[c.ID]["verlaengerung"]; ok {
			upsells = fmt.Sprintf("%dx verlängerung", n)
		}
		kommentare := strings.Join(data.CommentsByClient[c.ID], ", ")
		row := []string{c.Status, nachname, vorname, startFmt, endFmt, clvStr, c.Source, c.SourceStageName, upsells, kommentare}
		clientMap := data.AmountByClientMonth[c.ID]
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

// GET /api/exports/aggregated/cashflow.csv
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

	data, err := h.store.ExportAggregatedCashflow(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	setCSVHeaders(w, "cashflow_aggregated.csv")
	cw := csv.NewWriter(w)
	defer cw.Flush()

	if len(data.Clients) == 0 {
		_ = cw.Write([]string{"client_id", "client_name", "client_email", "client_phone", "client_source", "client_source_stage_name", "client_status", "first_contract_start", "last_contract_end", "total_revenue"})
		return
	}

	var minMonth, maxMonth time.Time
	for _, c := range data.Clients {
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

	header := []string{"client_id", "client_name", "client_email", "client_phone", "client_source", "client_source_stage_name", "client_status", "first_contract_start", "last_contract_end", "total_revenue"}
	for _, m := range months {
		header = append(header, monthHeaderKey(m))
	}
	_ = cw.Write(header)

	for _, c := range data.Clients {
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
		clientMap := data.AmountByClientMonth[c.ID]
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
