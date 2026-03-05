package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"time"
)

type ContractImport struct {
	Name          string                 `json:"name"`
	ContractStart string                 `json:"contract_start"`
	ContractEnd   string                 `json:"contract_end"`
	Cashflows     map[string]interface{} `json:"cashflows"`
	IsFormer      bool                   `json:"is_former"`
	//CLV           float64                `json:"clv"`

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
		// Derive revenue + due dates
		// -------------------------
		var dueDates []time.Time
		var revenueTotal float64

		for ym, value := range c.Cashflows {
			date, err := time.Parse("2006-01", ym)
			if err != nil {
				continue
			}

			if v, ok := value.(float64); ok && v > 0 {
				dueDates = append(dueDates, date)
				revenueTotal += v
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
		paymentFreq := "monthly"

		if len(dueDates) == 1 {
			paymentFreq = "one-time"
		} else if len(dueDates) >= 2 {
			sort.Slice(dueDates, func(i, j int) bool {
				return dueDates[i].Before(dueDates[j])
			})

			diff := (dueDates[1].Year()-dueDates[0].Year())*12 +
				int(dueDates[1].Month()-dueDates[0].Month())

			switch {
			case diff >= 6:
				paymentFreq = "bi-yearly"
			case diff >= 3:
				paymentFreq = "quarterly"
			case diff >= 2:
				paymentFreq = "bi-monthly"
			default:
				paymentFreq = "monthly"
			}
		}

		// -------------------------
		// Insert Contract
		// -------------------------
		contractID, _, err := h.createContractTx(r.Context(), tx, ContractCreateInput{
			ClientID:          clientID,
			StartDate:         start,
			EndDate:           &end,
			DurationMonths:    durationMonths,
			RevenueTotal:      revenueTotal,
			PaymentFreq:       paymentFreq,
			CreatedAtOverride: &createdAt,
			GenerateSchedule:  false,
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

		// -------------------------
		// Insert Cashflows + Comments
		// -------------------------
		if err := insertImportedCashflowEntriesTx(tx, contractID, c.Cashflows); err != nil {
			tx.Rollback()
			http.Error(w, err.Error(), 500)
			return
		}

		if err := tx.Commit(); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		imported++
	}

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
