-- Add created_at and updated_at to cashflow_entries and create trigger
ALTER TABLE cashflow_entries
  ADD COLUMN IF NOT EXISTS created_at TIMESTAMP DEFAULT now();

ALTER TABLE cashflow_entries
  ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP DEFAULT now();

-- create or replace trigger function to update updated_at
CREATE OR REPLACE FUNCTION trg_set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_trigger WHERE tgname = 'trg_cashflow_entries_set_updated_at'
  ) THEN
    CREATE TRIGGER trg_cashflow_entries_set_updated_at
    BEFORE UPDATE ON cashflow_entries
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();
  END IF;
END$$;
