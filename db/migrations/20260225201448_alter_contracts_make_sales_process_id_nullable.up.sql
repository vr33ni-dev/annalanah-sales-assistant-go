-- Allow contracts that were imported from legacy systems
-- to exist without a sales_process_id

ALTER TABLE contracts
ALTER COLUMN sales_process_id DROP NOT NULL;
