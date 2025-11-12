DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM information_schema.columns
    WHERE table_name = 'clients'
      AND column_name = 'completed_at'
  ) THEN
    ALTER TABLE clients DROP COLUMN completed_at;
  END IF;
END $$;
