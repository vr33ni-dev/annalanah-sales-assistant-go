BEGIN;

-- Normalize any invalid or out-of-spec values to safe defaults
UPDATE contracts
SET payment_frequency = 'monthly'
WHERE payment_frequency NOT IN ('monthly','bi-monthly','quarterly','one-time','bi-yearly')
   OR (payment_frequency = 'bi-yearly' AND COALESCE(duration_months, 0) < 12);

-- Drop the old constraint if present
ALTER TABLE contracts DROP CONSTRAINT IF EXISTS contracts_payment_frequency_check;

-- Add the new constraint (idempotent guard not strictly needed because we dropped it,
-- but keep the DO block to avoid errors if it exists concurrently)
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint c
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
