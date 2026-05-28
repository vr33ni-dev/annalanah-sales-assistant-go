// importer.go — HTTP handler for bulk data import: truncates all tables and re-imports from JSON payload.
package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/store"
)

type ContractImport struct {
	Name          string                 `json:"name"`
	Email         string                 `json:"email"`
	ContractStart string                 `json:"contract_start"`
	ContractEnd   string                 `json:"contract_end"`
	Cashflows     map[string]interface{} `json:"cashflows"`
	IsFormer      bool                   `json:"is_former"`
	IsRenewalRaw  string                 `json:"is_renewal_raw"`
	CLV           string                 `json:"clv"`
}

func (h *Handler) ImportContracts(w http.ResponseWriter, r *http.Request) {
	var payload []ContractImport

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if os.Getenv("APP_ENV") == "production" {
		http.Error(w, "migration import not allowed in production", http.StatusForbidden)
		return
	}

	if r.Header.Get("X-Migration-Key") != "ALLOW_MIGRATION" {
		http.Error(w, "invalid migration key", http.StatusForbidden)
		return
	}

	if err := h.store.TruncateAllTables(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	imported := 0
	skipped := []string{}

	for _, c := range payload {
		start, err := parseISO(c.ContractStart)
		if err != nil {
			if c.IsFormer {
				log.Printf("Skipping former client %s (invalid start)", c.Name)
				skipped = append(skipped, c.Name)
				continue
			}
			http.Error(w, "invalid contract_start", http.StatusBadRequest)
			return
		}

		end, err := parseISO(c.ContractEnd)
		if err != nil {
			if c.IsFormer {
				log.Printf("Skipping former client %s (invalid end)", c.Name)
				skipped = append(skipped, c.Name)
				continue
			}
			http.Error(w, "invalid contract_end", http.StatusBadRequest)
			return
		}

		if end.Before(start) {
			if c.IsFormer {
				log.Printf("Skipping former client %s (invalid date range)", c.Name)
				skipped = append(skipped, c.Name)
				continue
			}
			http.Error(w, "contract_end before contract_start", http.StatusBadRequest)
			return
		}

		status := "active"
		if c.IsFormer {
			status = "inactive"
		}

		createdAt := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
		revenueTotal := parseCLV(c.CLV)

		durationMonths := (end.Year()-start.Year())*12 + int(end.Month()-start.Month())
		if durationMonths <= 0 {
			durationMonths = 1
		}

		paymentFreq := detectPaymentFreq(c.Cashflows)
		isRenewal := strings.EqualFold(c.IsRenewalRaw, "ja")
		numPeriods := durationMonths / 6

		// Absorb empty trailing periods
		if isRenewal && numPeriods >= 2 {
			for numPeriods > 2 {
				lastPeriodStart := start.AddDate(0, (numPeriods-1)*6, 0)
				if !lastPeriodStart.Before(time.Now()) {
					break
				}
				lastPeriodStartMonth := time.Date(lastPeriodStart.Year(), lastPeriodStart.Month(), 1, 0, 0, 0, 0, time.UTC)
				hasPayment := false
				for ym, val := range c.Cashflows {
					t, err := time.Parse("2006-01", ym)
					if err != nil {
						continue
					}
					if v, ok := val.(float64); ok && v > 0 && !t.Before(lastPeriodStartMonth) {
						hasPayment = true
						break
					}
				}
				if !hasPayment {
					numPeriods--
				} else {
					break
				}
			}
		}

		var periods []store.PeriodContract
		if isRenewal && numPeriods >= 2 {
			for i := 0; i < numPeriods; i++ {
				periodStart := start.AddDate(0, i*6, 0)
				periodEnd := start.AddDate(0, (i+1)*6, 0)
				if i == numPeriods-1 {
					periodEnd = end
				}

				periodStartMonth := time.Date(periodStart.Year(), periodStart.Month(), 1, 0, 0, 0, 0, time.UTC)
				periodEndMonth := time.Date(periodEnd.Year(), periodEnd.Month(), 1, 0, 0, 0, 0, time.UTC)
				periodCashflows := map[string]interface{}{}
				for ym, val := range c.Cashflows {
					date, err := time.Parse("2006-01", ym)
					if err != nil {
						continue
					}
					if !date.Before(periodStartMonth) && date.Before(periodEndMonth) {
						periodCashflows[ym] = val
					}
				}

				periodDurationMonths := 6
				if i == numPeriods-1 {
					periodDurationMonths = (periodEnd.Year()-periodStart.Year())*12 + int(periodEnd.Month()-periodStart.Month())
					if periodDurationMonths <= 0 {
						periodDurationMonths = 6
					}
				}

				periods = append(periods, store.PeriodContract{
					Start:          periodStart,
					End:            periodEnd,
					DurationMonths: periodDurationMonths,
					Revenue:        revenueTotal / float64(numPeriods),
					PaymentFreq:    detectPaymentFreq(periodCashflows),
					Cashflows:      periodCashflows,
					IsUpsell:       i > 0,
					IsNonRenewal:   false,
				})
			}
		} else {
			periods = []store.PeriodContract{
				{
					Start:          start,
					End:            end,
					DurationMonths: durationMonths,
					Revenue:        revenueTotal,
					PaymentFreq:    paymentFreq,
					Cashflows:      c.Cashflows,
					IsUpsell:       false,
					IsNonRenewal:   !isRenewal,
				},
			}
		}

		err = h.store.ImportContractRecord(r.Context(), store.ImportContractInput{
			ClientName:      c.Name,
			Email:           c.Email,
			Status:          status,
			CreatedAt:       createdAt,
			IsFormer:        c.IsFormer,
			PeriodContracts: periods,
		})
		if err != nil {
			if c.IsFormer {
				log.Printf("Skipping former client %s (import failed: %v)", c.Name, err)
				skipped = append(skipped, c.Name)
				continue
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		imported++
	}

	log.Printf("ImportContracts: imported=%d skipped=%d", imported, len(skipped))

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "import completed",
		"imported": imported,
		"skipped":  skipped,
	})
}

func parseISO(value string) (time.Time, error) {
	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, value); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid date: %s", value)
}

func detectPaymentFreq(cashflows map[string]interface{}) string {
	var dueDates []time.Time
	for ym, value := range cashflows {
		date, err := time.Parse("2006-01", ym)
		if err != nil {
			continue
		}
		if v, ok := value.(float64); ok && v > 0 {
			dueDates = append(dueDates, date)
		}
	}

	if len(dueDates) == 0 {
		return "monthly"
	}
	if len(dueDates) == 1 {
		return "one-time"
	}

	sort.Slice(dueDates, func(i, j int) bool {
		return dueDates[i].Before(dueDates[j])
	})

	diff := (dueDates[1].Year()-dueDates[0].Year())*12 +
		int(dueDates[1].Month()-dueDates[0].Month())

	switch {
	case diff >= 6:
		return "bi-yearly"
	case diff >= 3:
		return "quarterly"
	case diff >= 2:
		return "bi-monthly"
	default:
		return "monthly"
	}
}

func parseCLV(raw string) float64 {
	s := strings.TrimSpace(raw)
	s = strings.ReplaceAll(s, "€", "")
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, ",", ".")
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	if v < 100 {
		return v * 1000
	}
	return v
}
