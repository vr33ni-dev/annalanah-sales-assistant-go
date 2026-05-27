-- Drop trigger/function and remove timestamp columns from cashflow_entries
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = 'trg_cashflow_entries_set_updated_at') THEN
    DROP TRIGGER trg_cashflow_entries_set_updated_at ON cashflow_entries;
  END IF;
END$$;

DROP FUNCTION IF EXISTS trg_set_updated_at();

ALTER TABLE cashflow_entries
  DROP COLUMN IF EXISTS updated_at;

ALTER TABLE cashflow_entries
  DROP COLUMN IF EXISTS created_at;
