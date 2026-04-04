package api

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// insertCashflowEntriesTx inserts scheduled cashflow entries for a contract inside the provided transaction.
// It is idempotent when a UNIQUE(contract_id,due_date) index exists (uses ON CONFLICT DO NOTHING).
func insertCashflowEntriesTx(
	tx *sql.Tx,
	contractID int,
	startDate time.Time,
	endDate time.Time,
	revenueTotal float64,
	paymentFreq string,
) error {

	if endDate.Before(startDate) {
		return fmt.Errorf("endDate cannot be before startDate")
	}

	// Determine month step based on frequency
	step := 1
	switch paymentFreq {
	case "monthly":
		step = 1
	case "bi-monthly":
		step = 2
	case "quarterly":
		step = 3
	case "bi-yearly":
		step = 6
	case "one-time":
		step = 0
	default:
		step = 1
	}

	stmt := `
		INSERT INTO cashflow_entries 
			(contract_id, due_date, amount, status) 
		VALUES ($1, $2::date, $3, 'confirmed')
		ON CONFLICT (contract_id, due_date) DO NOTHING
	`

	ctx := context.Background()

	// ONE-TIME PAYMENT
	if paymentFreq == "one-time" {
		_, err := tx.ExecContext(ctx, stmt,
			contractID,
			startDate.Format("2006-01-02"),
			revenueTotal,
		)
		return err
	}

	// Count number of full periods where the next boundary still fits in contract range.
	// This models "pay at period start" semantics.
	periods := 0
	current := startDate
	for {
		next := addMonthClamped(current, step)
		if next.After(endDate) {
			break
		}
		periods++
		current = next
	}

	if periods == 0 {
		return nil
	}

	// Evenly distribute revenue
	amountPerPeriod := revenueTotal / float64(periods)

	// Reset loop
	current = startDate
	for i := 0; i < periods; i++ {

		_, err := tx.ExecContext(ctx, stmt,
			contractID,
			current.Format("2006-01-02"),
			amountPerPeriod,
		)
		if err != nil {
			return err
		}

		current = addMonthClamped(current, step)
	}

	return nil
}

func addMonthClamped(t time.Time, months int) time.Time {
	year, month, day := t.Date()
	loc := t.Location()

	// target month
	target := time.Date(year, month+time.Month(months), 1, 0, 0, 0, 0, loc)

	// last day of target month
	lastDay := target.AddDate(0, 1, -1).Day()

	if day > lastDay {
		day = lastDay
	}

	return time.Date(
		target.Year(),
		target.Month(),
		day,
		0, 0, 0, 0,
		loc,
	)
}

// insertImportedCashflowEntriesTx inserts imported contract cashflow rows and notes as comments.
// It preserves importer semantics: numeric values become cashflow entries, mixed/non-numeric
// strings become comments, and placeholders like "-" are ignored.
func insertImportedCashflowEntriesTx(tx *sql.Tx, contractID int, clientID int, cashflows map[string]interface{}) error {
	numRe := regexp.MustCompile(`[-+]?[0-9]*\.?[0-9]+`)

	for ym, value := range cashflows {
		date, err := time.Parse("2006-01", ym)
		if err != nil {
			continue
		}

		switch v := value.(type) {
		case float64:
			if v == 0 {
				continue
			}
			// kEUR scaling: values < 100 are in kEUR (e.g. 1.8 = €1800)
			if v < 100 {
				v *= 1000
			}
			if _, err := tx.Exec(`
				INSERT INTO cashflow_entries
					(contract_id, due_date, amount, status)
				VALUES ($1, $2::date, $3, 'confirmed')
			`, contractID, date, v); err != nil {
				return err
			}

		case string:
			trimmed := strings.TrimSpace(v)
			if trimmed == "" {
				continue
			}
			if strings.ToLower(trimmed) == "-" {
				continue
			}

			numStr := numRe.FindString(trimmed)
			if numStr != "" {
				if n, err := strconv.ParseFloat(numStr, 64); err == nil && n != 0 {
					// kEUR scaling: values < 100 are in kEUR
					if n < 100 {
						n *= 1000
					}
					if _, err := tx.Exec(`
						INSERT INTO cashflow_entries
							(contract_id, due_date, amount, status)
					VALUES ($1, $2::date, $3, 'confirmed')
					`, contractID, date, n); err != nil {
						return err
					}
				}

				leftover := strings.TrimSpace(strings.Replace(trimmed, numStr, "", 1))
				if leftover != "" {
					commentBody := fmt.Sprintf("%s: %s", ym, trimmed)
					if _, err := tx.Exec(`
						INSERT INTO comments (entity_type, entity_id, body, author)
						VALUES ('client', $1, $2, 'importer')
					`, clientID, commentBody); err != nil {
						return err
					}
				}
				continue
			}

			commentBody := fmt.Sprintf("%s: %s", ym, trimmed)
			if _, err := tx.Exec(`
				INSERT INTO comments (entity_type, entity_id, body, author)
				VALUES ('client', $1, $2, 'importer')
			`, clientID, commentBody); err != nil {
				return err
			}
		}
	}

	return nil
}
