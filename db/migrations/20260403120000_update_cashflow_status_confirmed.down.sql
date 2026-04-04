-- Revert: restore (pending, paid, overdue) constraint and rename confirmed → pending.

ALTER TABLE cashflow_entries
    DROP CONSTRAINT cashflow_entries_status_check;

ALTER TABLE cashflow_entries
    ALTER COLUMN status SET DEFAULT 'pending';

UPDATE cashflow_entries
    SET status = 'pending'
    WHERE status = 'confirmed';

ALTER TABLE cashflow_entries
    ADD CONSTRAINT cashflow_entries_status_check
        CHECK (status IN ('pending', 'paid', 'overdue'));
