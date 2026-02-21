-- Rollback add_updated_at_to_contracts_and_sales_process

-- remove triggers and functions
DROP TRIGGER IF EXISTS contracts_set_updated_at ON contracts;
DROP FUNCTION IF EXISTS contracts_updated_at_trigger();

-- drop column
ALTER TABLE contracts DROP COLUMN IF EXISTS updated_at;

-- sales_process rollback
DROP TRIGGER IF EXISTS sales_process_set_updated_at ON sales_process;
DROP FUNCTION IF EXISTS sales_process_updated_at_trigger();
ALTER TABLE sales_process DROP COLUMN IF EXISTS updated_at;
