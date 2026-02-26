package api

import (
	"context"
	"database/sql"
	"fmt"
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
		VALUES ($1, $2::date, $3, 'pending')
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

	// Count number of periods
	periods := 0
	current := startDate

	for !current.After(endDate) {
		periods++
		current = addMonthClamped(current, step)
	}

	if periods == 0 {
		return nil
	}

	// Evenly distribute revenue
	amountPerPeriod := revenueTotal / float64(periods)

	// Reset loop
	current = startDate

	for !current.After(endDate) {

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
