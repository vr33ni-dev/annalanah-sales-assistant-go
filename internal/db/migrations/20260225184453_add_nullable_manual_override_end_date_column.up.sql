-- 1. Drop generated column
ALTER TABLE contracts
  DROP COLUMN IF EXISTS end_date_computed;

-- 2. Add explicit end_date column
ALTER TABLE contracts
  ADD COLUMN end_date DATE;
