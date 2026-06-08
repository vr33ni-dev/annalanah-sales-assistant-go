-- create unique index to support ON CONFLICT on cashflow_entries(contract_id,due_date)
CREATE UNIQUE INDEX IF NOT EXISTS ux_cashflow_contract_due ON cashflow_entries (contract_id, due_date);
