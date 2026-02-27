-- ============================
-- Revert cashflow index changes
-- ============================

-- Recreate old indexes
CREATE INDEX IF NOT EXISTS idx_cashflow_due_date
ON cashflow_entries (due_date);

CREATE INDEX IF NOT EXISTS idx_cashflow_entries_due_status
ON cashflow_entries (due_date, status);

-- Remove new composite indexes
DROP INDEX IF EXISTS idx_cashflow_contract_due;
DROP INDEX IF EXISTS idx_cashflow_contract_status_due;