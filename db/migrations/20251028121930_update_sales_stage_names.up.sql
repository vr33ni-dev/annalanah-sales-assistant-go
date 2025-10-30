-- 20251028121930_update_sales_stage_names.up.sql

UPDATE sales_process
SET stage = 'Zweitgespraech'
WHERE LOWER(stage) IN ('zweitgespraech', 'zweitgespräch');

UPDATE sales_process
SET stage = 'Abschluss'
WHERE LOWER(stage) = ('abschluss');

UPDATE sales_process
SET stage = 'Verloren'
WHERE LOWER(stage) = 'lost';
