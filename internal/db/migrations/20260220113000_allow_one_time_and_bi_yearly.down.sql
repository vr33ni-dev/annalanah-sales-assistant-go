BEGIN;

-- Revert any 'one-time' or 'bi-yearly' values back to 'monthly' so the old CHECK will be satisfied
UPDATE contracts
SET payment_frequency = 'monthly'
WHERE payment_frequency NOT IN ('monthly','bi-monthly','quarterly');

-- Drop the new constraint if present
ALTER TABLE contracts DROP CONSTRAINT IF EXISTS contracts_payment_frequency_check;

-- Recreate the original, stricter constraint
ALTER TABLE contracts
  ADD CONSTRAINT contracts_payment_frequency_check CHECK (payment_frequency IN ('monthly','bi-monthly','quarterly'));

COMMIT;
