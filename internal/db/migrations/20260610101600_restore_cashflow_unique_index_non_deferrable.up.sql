-- The deferrable variant was applied to some DBs as a table constraint (not just
-- an index), which breaks ON CONFLICT (contract_id, due_date) DO NOTHING.
-- Drop the constraint (if present), then drop the bare index (if present),
-- and recreate as a plain non-deferrable unique index.
ALTER TABLE cashflow_entries DROP CONSTRAINT IF EXISTS ux_cashflow_contract_due;
DROP INDEX IF EXISTS ux_cashflow_contract_due;
CREATE UNIQUE INDEX ux_cashflow_contract_due ON cashflow_entries (contract_id, due_date);
