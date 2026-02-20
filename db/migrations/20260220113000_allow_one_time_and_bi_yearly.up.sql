BEGIN;

-- Ensure any existing 'bi-yearly' rows with duration < 12 months are normalized
UPDATE contracts
SET payment_frequency = 'monthly'
WHERE payment_frequency = 'bi-yearly'
  AND COALESCE(duration_months, 0) < 12;

-- Drop any existing CHECK constraint that restricts payment_frequency via an IN(...) clause
DO $$
DECLARE r RECORD;
BEGIN
  FOR r IN
    SELECT conname
    FROM pg_constraint c
    JOIN pg_class t ON c.conrelid = t.oid
    WHERE t.relname = 'contracts'
      AND pg_get_constraintdef(c.oid) LIKE '%payment_frequency IN (%'
  LOOP
    EXECUTE format('ALTER TABLE contracts DROP CONSTRAINT %I', r.conname);
  END LOOP;
END $$;

-- Add new named constraint allowing one-time and bi-yearly (bi-yearly only when duration >= 12 months)
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint c
    WHERE c.conname = 'contracts_payment_frequency_check'
      AND c.conrelid = 'contracts'::regclass
  ) THEN
    ALTER TABLE contracts
      ADD CONSTRAINT contracts_payment_frequency_check CHECK (
        payment_frequency IN ('monthly','bi-monthly','quarterly','one-time','bi-yearly')
        AND (payment_frequency != 'bi-yearly' OR COALESCE(duration_months, 0) >= 12)
      );
  END IF;
END$$;

COMMIT;
