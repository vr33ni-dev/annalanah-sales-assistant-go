-- 20251028121930_update_sales_stage_names.down.sql
-- Rollback stage name normalization

-- 1. Drop the new constraint
ALTER TABLE sales_process
DROP CONSTRAINT IF EXISTS sales_process_stage_check;

-- 2. Recreate original lowercase-only constraint
ALTER TABLE sales_process
ADD CONSTRAINT sales_process_stage_check
CHECK (stage IN ('zweitgespraech', 'abschluss', 'lost'));

-- 3. Revert data to original values
UPDATE sales_process
SET stage = 'zweitgespraech'
WHERE stage = 'Zweitgespraech';

UPDATE sales_process
SET stage = 'abschluss'
WHERE stage = 'Abschluss';

UPDATE sales_process
SET stage = 'lost'
WHERE stage = 'Verloren';
