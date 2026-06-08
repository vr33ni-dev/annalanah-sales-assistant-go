ALTER TABLE contracts
  DROP COLUMN end_date;

ALTER TABLE contracts
  ADD COLUMN end_date_computed DATE GENERATED ALWAYS AS (
    (start_date + make_interval(months => duration_months))::date
  ) STORED;
