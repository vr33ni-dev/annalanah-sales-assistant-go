package api

import (
    "context"
    "database/sql"
    "time"
)

// insertCashflowEntriesTx inserts scheduled cashflow entries for a contract inside the provided transaction.
// It is idempotent when a UNIQUE(contract_id,due_date) index exists (uses ON CONFLICT DO NOTHING).
func insertCashflowEntriesTx(tx *sql.Tx, contractID int, startDate time.Time, durationMonths int, revenueTotal float64, paymentFreq string) error {
    // determine step in months
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
        // for one-time payments we create a single entry at start_date
        step = durationMonths
    default:
        step = 1
    }

    // per-month base
    perMonth := 0.0
    if durationMonths > 0 {
        perMonth = revenueTotal / float64(durationMonths)
    }

    // insert entries for each generated due date within the contract duration
    stmt := `INSERT INTO cashflow_entries (contract_id, due_date, amount, status) VALUES ($1, $2::date, $3, 'pending') ON CONFLICT (contract_id, due_date) DO NOTHING`

    ctx := context.Background()
    for offset := 0; offset < durationMonths; offset += step {
        due := startDate.AddDate(0, offset, 0)
        amount := perMonth * float64(step)
        if paymentFreq == "one-time" {
            amount = revenueTotal
        }
        if _, err := tx.ExecContext(ctx, stmt, contractID, due.Format("2006-01-02"), amount); err != nil {
            return err
        }
    }

    return nil
}
