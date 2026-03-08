-- Add is_imported placeholder column
ALTER TABLE sales_process
  ADD COLUMN is_imported_placeholder BOOLEAN NOT NULL DEFAULT FALSE;
