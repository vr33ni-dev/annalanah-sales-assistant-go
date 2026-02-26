-- Re-enforce sales_process_id requirement
-- WARNING: This will fail if any contracts have NULL sales_process_id

ALTER TABLE contracts
ALTER COLUMN sales_process_id SET NOT NULL;
