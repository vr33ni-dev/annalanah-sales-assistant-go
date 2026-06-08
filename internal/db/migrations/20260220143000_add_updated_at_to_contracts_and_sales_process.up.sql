-- Add updated_at to contracts and sales_process and triggers to refresh on updates

ALTER TABLE contracts
  ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP DEFAULT now();


-- Trigger function to refresh updated_at on update
CREATE OR REPLACE FUNCTION contracts_updated_at_trigger()
RETURNS TRIGGER AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- (re)create trigger
DROP TRIGGER IF EXISTS contracts_set_updated_at ON contracts;
CREATE TRIGGER contracts_set_updated_at
BEFORE UPDATE ON contracts
FOR EACH ROW
EXECUTE FUNCTION contracts_updated_at_trigger();

-- ------------------------------
-- Also add updated_at for sales_process
-- ------------------------------
ALTER TABLE sales_process
  ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP DEFAULT now();

-- (no backfill for sales_process; migration only adds column + trigger)

CREATE OR REPLACE FUNCTION sales_process_updated_at_trigger()
RETURNS TRIGGER AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS sales_process_set_updated_at ON sales_process;
CREATE TRIGGER sales_process_set_updated_at
BEFORE UPDATE ON sales_process
FOR EACH ROW
EXECUTE FUNCTION sales_process_updated_at_trigger();
