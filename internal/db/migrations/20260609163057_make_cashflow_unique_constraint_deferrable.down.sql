ALTER TABLE cashflow_entries DROP CONSTRAINT IF EXISTS ux_cashflow_contract_due;

CREATE UNIQUE INDEX IF NOT EXISTS ux_cashflow_contract_due ON cashflow_entries (contract_id, due_date);
