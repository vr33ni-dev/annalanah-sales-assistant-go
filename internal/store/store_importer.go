package store

import (
	"context"
)

func (s *PostgresStore) TruncateAllTables(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		TRUNCATE TABLE cashflow_entries, comments, contracts, clients
		RESTART IDENTITY CASCADE;`)
	return err
}

func (s *PostgresStore) ImportContractRecord(ctx context.Context, in ImportContractInput) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	createdAt := in.CreatedAt

	var clientID int
	var emailArg interface{}
	if in.Email != "" {
		emailArg = in.Email
	}
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO clients (name, email, status, created_at)
		VALUES ($1, $2, $3, $4) RETURNING id`,
		in.ClientName, emailArg, in.Status, createdAt,
	).Scan(&clientID); err != nil {
		return err
	}

	var salesProcessID int
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO sales_process (client_id, stage, follow_up_date, follow_up_result, closed, revenue, stage_id, created_at, updated_at, is_imported_placeholder)
		VALUES ($1, NULL, NULL, NULL, NULL, NULL, NULL, $2, $2, TRUE) RETURNING id`,
		clientID, createdAt,
	).Scan(&salesProcessID); err != nil {
		return err
	}

	prevContractID := 0
	for i, p := range in.PeriodContracts {
		contractID, _, err := s.createContractTx(ctx, tx, ContractCreateInput{
			ClientID:          clientID,
			SalesProcessID:    &salesProcessID,
			StartDate:         p.Start,
			EndDate:           &p.End,
			DurationMonths:    p.DurationMonths,
			RevenueTotal:      p.Revenue,
			PaymentFreq:       p.PaymentFreq,
			CreatedAtOverride: &createdAt,
			GenerateSchedule:  false,
			Source:            "imported",
		})
		if err != nil {
			return err
		}

		if err := insertImportedCashflowEntriesTx(tx, contractID, clientID, p.Cashflows); err != nil {
			return err
		}

		if i > 0 && p.IsUpsell {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO contract_upsells
				    (sales_process_id, client_id, upsell_date, upsell_result, upsell_revenue, previous_contract_id, new_contract_id)
				VALUES ($1, $2, $3, 'verlaengerung', $4, $5, $6)`,
				salesProcessID, clientID, p.Start, p.Revenue, prevContractID, contractID); err != nil {
				return err
			}
		}

		if p.IsNonRenewal {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO contract_upsells
				    (sales_process_id, client_id, upsell_date, upsell_result, upsell_revenue, previous_contract_id, new_contract_id)
				VALUES ($1, $2, NULL, 'keine_verlaengerung', $3, $4, NULL)`,
				salesProcessID, clientID, p.Revenue, contractID); err != nil {
				return err
			}
		}

		prevContractID = contractID
	}

	return tx.Commit()
}
