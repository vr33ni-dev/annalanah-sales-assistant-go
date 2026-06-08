-- Rollback: remove converted fields
DROP INDEX IF EXISTS idx_leads_converted;

ALTER TABLE leads
  DROP COLUMN IF EXISTS converted_client_id,
  DROP COLUMN IF EXISTS converted_at,
  DROP COLUMN IF EXISTS converted;