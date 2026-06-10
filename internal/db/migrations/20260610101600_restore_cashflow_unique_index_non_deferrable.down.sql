DROP INDEX IF EXISTS ux_cashflow_contract_due;
CREATE UNIQUE INDEX ux_cashflow_contract_due ON cashflow_entries (contract_id, due_date);
