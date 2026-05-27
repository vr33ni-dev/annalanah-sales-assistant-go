ALTER TABLE contracts
  ALTER COLUMN duration_months DROP NOT NULL;

ALTER TABLE contracts
  DROP CONSTRAINT IF EXISTS contracts_duration_months_check;

ALTER TABLE contracts
  ADD CONSTRAINT contracts_duration_months_check
    CHECK (duration_months IS NULL OR duration_months >= 0);