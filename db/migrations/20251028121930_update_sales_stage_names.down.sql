-- 20251028121930_update_sales_stage_names.down.sql

UPDATE sales_process
SET stage = 'zweitgespraech'
WHERE stage = 'Zweitgespraech';

UPDATE sales_process
SET stage = 'abschluss'
WHERE stage = 'Abschluss';

UPDATE sales_process
SET stage = 'lost'
WHERE stage = 'Verloren';

