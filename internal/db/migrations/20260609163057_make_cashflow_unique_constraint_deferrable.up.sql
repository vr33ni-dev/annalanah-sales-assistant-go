-- Replace the immediate unique index with a deferrable unique constraint.
-- The immediate index causes row-by-row uniqueness checks during UPDATE, which
-- fails when shifting cashflow dates forward (entry A lands on entry B's current
-- date before B gets shifted). A deferred constraint checks at commit time instead.
DROP INDEX IF EXISTS ux_cashflow_contract_due;

ALTER TABLE cashflow_entries
  ADD CONSTRAINT ux_cashflow_contract_due
  UNIQUE (contract_id, due_date)
  DEFERRABLE INITIALLY DEFERRED;
