-- Rollback: remove lead_id and its index
DROP INDEX IF EXISTS idx_sales_process_lead_id;

ALTER TABLE sales_process
  DROP COLUMN IF EXISTS lead_id;