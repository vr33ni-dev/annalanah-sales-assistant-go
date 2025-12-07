-- Drop trigger
DROP TRIGGER IF EXISTS set_timestamp_contract_upsells ON contract_upsells;

-- Do NOT drop the function, because it may be used by other tables.
-- But if you want a strict clean rollback:
-- DROP FUNCTION IF EXISTS trigger_set_timestamp();

-- Remove updated_at column
ALTER TABLE contract_upsells
DROP COLUMN IF EXISTS updated_at;
