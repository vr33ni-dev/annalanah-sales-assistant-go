-- 20251028121930_update_sales_stage_names.up.sql

-- 1. Drop the old constraint
ALTER TABLE sales_process
DROP CONSTRAINT IF EXISTS sales_process_stage_check;

-- 2. Add a NOT VALID constraint first (so it doesn't check existing rows yet)
ALTER TABLE sales_process
ADD CONSTRAINT sales_process_stage_check
CHECK (stage IN ('Zweitgespraech', 'Abschluss', 'Verloren'))
NOT VALID;

-- 3. Update existing data to match new allowed values
UPDATE sales_process
SET stage = 'Zweitgespraech'
WHERE LOWER(stage) IN ('zweitgespraech', 'zweitgespräch');

UPDATE sales_process
SET stage = 'Abschluss'
WHERE LOWER(stage) = 'abschluss';

UPDATE sales_process
SET stage = 'Verloren'
WHERE LOWER(stage) IN ('lost', 'verloren');

-- 4. Now that all rows match, validate the constraint
ALTER TABLE sales_process
VALIDATE CONSTRAINT sales_process_stage_check;
