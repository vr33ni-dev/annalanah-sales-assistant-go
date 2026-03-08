-- Remove is_imported placeholder column
ALTER TABLE sales_process
  DROP COLUMN IF EXISTS is_imported_placeholder;

