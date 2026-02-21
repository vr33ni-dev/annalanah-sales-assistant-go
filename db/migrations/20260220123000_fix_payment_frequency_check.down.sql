BEGIN;

-- Restore previous (stricter) constraint and normalize values so it will apply cleanly
UPDATE contracts
SET payment_frequency = 'monthly'
WHERE payment_frequency NOT IN ('monthly','bi-monthly','quarterly');

ALTER TABLE contracts DROP CONSTRAINT IF EXISTS contracts_payment_frequency_check;

ALTER TABLE contracts
  ADD CONSTRAINT contracts_payment_frequency_check CHECK (payment_frequency IN ('monthly','bi-monthly','quarterly'));

COMMIT;
