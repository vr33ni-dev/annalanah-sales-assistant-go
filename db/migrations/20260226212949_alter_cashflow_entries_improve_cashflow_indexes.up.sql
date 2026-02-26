-- ============================
-- Improve cashflow indexes
-- ============================

-- 1. Add better composite indexes
CREATE INDEX IF NOT EXISTS idx_cashflow_contract_due
ON cashflow_entries (contract_id, due_date);

CREATE INDEX IF NOT EXISTS idx_cashflow_contract_status_due
ON cashflow_entries (contract_id, status, due_date);

-- 2. Remove weaker redundant ones
DROP INDEX IF EXISTS idx_cashflow_due_date;
DROP INDEX IF EXISTS idx_cashflow_entries_due_status;