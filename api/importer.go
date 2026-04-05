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
)

type ContractImport struct {
	Name          string                 `json:"name"`
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

	// -------------------------------------------------
	// 🚨 MIGRATION MODE: CLEAR DATABASE FIRST
	// -------------------------------------------------

	// Optional: Block in production
	if os.Getenv("APP_ENV") == "production" {
		http.Error(w, "migration import not allowed in production", http.StatusForbidden)
		return
	}

	// Require special header to prevent accidents
	if r.Header.Get("X-Migration-Key") != "ALLOW_MIGRATION" {
		http.Error(w, "invalid migration key", http.StatusForbidden)
		return
	}

	_, err := h.DB.Exec(`
		TRUNCATE TABLE 
			cashflow_entries,
			comments,
			contracts,
			clients
		RESTART IDENTITY CASCADE;
	`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	imported := 0
	skipped := []string{}

	for _, c := range payload {

		tx, err := h.DB.Begin()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		// -------------------------
		// Parse Dates
		// -------------------------
		start, err := parseISO(c.ContractStart)
		if err != nil {
			if c.IsFormer {
				log.Printf("Skipping former client %s (invalid start)", c.Name)
				tx.Rollback()
				skipped = append(skipped, c.Name)
				continue
			}
			tx.Rollback()
			http.Error(w, "invalid contract_start", 400)
			return
		}

		end, err := parseISO(c.ContractEnd)
		if err != nil {
			if c.IsFormer {
				log.Printf("Skipping former client %s (invalid end)", c.Name)
				tx.Rollback()
				skipped = append(skipped, c.Name)
				continue
			}
			tx.Rollback()
			http.Error(w, "invalid contract_end", 400)
			return
		}

		if end.Before(start) {
			if c.IsFormer {
				log.Printf("Skipping former client %s (invalid date range)", c.Name)
				tx.Rollback()
				skipped = append(skipped, c.Name)
				continue
			}
			tx.Rollback()
			http.Error(w, "contract_end before contract_start", 400)
			return
		}

		// -------------------------
		// Insert Client
		// -------------------------
		status := "active"
		if c.IsFormer {
			status = "inactive"
		}

		// Derive a stable created_at from the contract start date so imported
		// records don't look "new" just because they were inserted today.
		createdAt := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)

		var clientID int
		err = tx.QueryRow(`
			INSERT INTO clients (name, status, created_at)
			VALUES ($1, $2, $3)
			RETURNING id
		`, c.Name, status, createdAt).Scan(&clientID)

		if err != nil {
			tx.Rollback()
			if c.IsFormer {
				log.Printf("Skipping former client %s (client insert failed)", c.Name)
				skipped = append(skipped, c.Name)
				continue
			}
			http.Error(w, err.Error(), 500)
			return
		}

		// -------------------------
		// Insert placeholder Sales Process
		// -------------------------
		var salesProcessID int
		err = tx.QueryRow(`
			INSERT INTO sales_process (
				client_id,
				stage,
				follow_up_date,
				follow_up_result,
				closed,
				revenue,
				stage_id,
				created_at,
				updated_at,
				is_imported_placeholder
			)
			VALUES ($1, NULL, NULL, NULL, NULL, NULL, NULL, $2, $2, TRUE)
			RETURNING id
		`, clientID, createdAt).Scan(&salesProcessID)

		if err != nil {
			tx.Rollback()
			if c.IsFormer {
				log.Printf("Skipping former client %s (placeholder sales_process insert failed)", c.Name)
				skipped = append(skipped, c.Name)
				continue
			}
			http.Error(w, err.Error(), 500)
			return
		}

		// -------------------------
		// Derive revenue from CLV (format: "€7.20" = 7200)
		// -------------------------
		revenueTotal := parseCLV(c.CLV)

		// Still collect due dates for payment frequency detection
		var dueDates []time.Time
		for ym, value := range c.Cashflows {
			date, err := time.Parse("2006-01", ym)
			if err != nil {
				continue
			}
			if v, ok := value.(float64); ok && v > 0 {
				dueDates = append(dueDates, date)
			}
		}

		// -------------------------
		// Compute duration
		// -------------------------
		durationMonths := (end.Year()-start.Year())*12 + int(end.Month()-start.Month())
		if durationMonths <= 0 {
			durationMonths = 1
		}

		// -------------------------
		// Detect payment frequency
		// -------------------------
		paymentFreq := detectPaymentFreq(c.Cashflows)

		// -------------------------
		// Renewal: split into 6-month periods with upsells
		// -------------------------
		isRenewal := strings.EqualFold(c.IsRenewalRaw, "ja")
		numPeriods := durationMonths / 6

		// If the last period hasn't started yet OR has started but contains no confirmed
		// payments (only "?"), absorb it into the previous period so the visible contract
		// always has real history and future-only contracts are not shown prematurely.
		if isRenewal && numPeriods >= 2 {
			for numPeriods > 2 {
				lastPeriodStart := start.AddDate(0, (numPeriods-1)*6, 0)
				// Future period: absorb unconditionally (it hasn't started yet)
				if !lastPeriodStart.Before(time.Now()) {
					numPeriods--
					continue
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

		if isRenewal && numPeriods >= 2 {
			// Split the contract into 6-month periods.
			// Period 0 = original contract; periods 1..n-1 = upsells (verlaengerung).
			prevContractID := 0

			for i := 0; i < numPeriods; i++ {
				periodStart := start.AddDate(0, i*6, 0)
				periodEnd := start.AddDate(0, (i+1)*6, 0)
				if i == numPeriods-1 {
					periodEnd = end
				}

				// Filter cashflows that fall within this period [periodStart, periodEnd)
				// Use month-level comparison: cashflows are month-granular (first of month),
				// but period boundaries may fall mid-month (e.g. Sep 3). Truncating to
				// first-of-month ensures "2025-09" goes to the period whose start month is Sep,
				// not the one that ends on Sep 3.
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

				// Revenue per period: CLV split evenly across periods.
				// Cashflow entries track observed payments separately and may be incomplete.
				periodRevenue := revenueTotal / float64(numPeriods)

				periodPaymentFreq := detectPaymentFreq(periodCashflows)
				contractID, _, err := h.createContractTx(r.Context(), tx, ContractCreateInput{
					ClientID:          clientID,
					SalesProcessID:    &salesProcessID,
					StartDate:         periodStart,
					EndDate:           &periodEnd,
					DurationMonths:    periodDurationMonths,
					RevenueTotal:      periodRevenue,
					PaymentFreq:       periodPaymentFreq,
					CreatedAtOverride: &createdAt,
					GenerateSchedule:  false,
					Source:            "imported",
				})
				if err != nil {
					tx.Rollback()
					if c.IsFormer {
						log.Printf("Skipping former client %s (contract insert failed period %d)", c.Name, i)
						skipped = append(skipped, c.Name)
						goto nextRecord
					}
					http.Error(w, err.Error(), 500)
					return
				}

				if err := insertImportedCashflowEntriesTx(tx, contractID, clientID, periodCashflows); err != nil {
					tx.Rollback()
					http.Error(w, err.Error(), 500)
					return
				}

				if i > 0 {
					// Link previous period to this one via an upsell record
					if _, err := tx.Exec(`
						INSERT INTO contract_upsells
							(sales_process_id, client_id, upsell_date, upsell_result, upsell_revenue, previous_contract_id, new_contract_id)
						VALUES ($1, $2, $3, 'verlaengerung', $4, $5, $6)
					`, salesProcessID, clientID, periodStart, periodRevenue, prevContractID, contractID); err != nil {
						tx.Rollback()
						http.Error(w, "upsell insert failed: "+err.Error(), 500)
						return
					}
				}

				prevContractID = contractID
			}
		} else {
			// Single contract (not a renewal, or duration < 12 months)
			contractID, _, err := h.createContractTx(r.Context(), tx, ContractCreateInput{
				ClientID:          clientID,
				SalesProcessID:    &salesProcessID,
				StartDate:         start,
				EndDate:           &end,
				DurationMonths:    durationMonths,
				RevenueTotal:      revenueTotal,
				PaymentFreq:       paymentFreq,
				CreatedAtOverride: &createdAt,
				GenerateSchedule:  false,
				Source:            "imported",
			})

			if err != nil {
				tx.Rollback()
				if c.IsFormer {
					log.Printf("Skipping former client %s (contract insert failed)", c.Name)
					skipped = append(skipped, c.Name)
					continue
				}
				http.Error(w, err.Error(), 500)
				return
			}

			if err := insertImportedCashflowEntriesTx(tx, contractID, clientID, c.Cashflows); err != nil {
				tx.Rollback()
				http.Error(w, err.Error(), 500)
				return
			}

			// Non-renewal: record a keine_verlaengerung upsell so it appears in analytics.
			// upsell_date is NULL because the importer doesn't provide a renewal date.
			if !isRenewal {
				if _, err := tx.Exec(`
					INSERT INTO contract_upsells
						(sales_process_id, client_id, upsell_date, upsell_result, upsell_revenue, previous_contract_id, new_contract_id)
					VALUES ($1, $2, NULL, 'keine_verlaengerung', $3, $4, NULL)
				`, salesProcessID, clientID, revenueTotal, contractID); err != nil {
					tx.Rollback()
					http.Error(w, "keine_verlaengerung insert failed: "+err.Error(), 500)
					return
				}
			}
		}

		if err := tx.Commit(); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		imported++
		continue

	nextRecord:
		// jumped here when a former-client record was skipped mid-loop
	}

	log.Printf("ImportContracts: imported=%d skipped=%d", imported, len(skipped))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
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

// parseCLV converts the CLV string (e.g. "€7.20") to a float64 in EUR.
// Values < 100 are stored in kEUR and are multiplied by 1000 ("€7.20" → 7200).
// Values >= 100 are already in EUR and are used as-is ("€900.00" → 900).
func parseCLV(raw string) float64 {
	s := strings.TrimSpace(raw)
	s = strings.ReplaceAll(s, "€", "")
	s = strings.TrimSpace(s)
	// European decimal: replace comma with dot
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
