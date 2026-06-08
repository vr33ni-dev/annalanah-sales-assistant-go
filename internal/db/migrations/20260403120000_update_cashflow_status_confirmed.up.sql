-- Migrate cashflow_entries status from (pending, paid, overdue) to (confirmed, overdue).
-- pending → confirmed (scheduled, not yet paid)
-- paid   → confirmed (settled, treated the same as scheduled)

ALTER TABLE cashflow_entries
    DROP CONSTRAINT cashflow_entries_status_check;

ALTER TABLE cashflow_entries
    ALTER COLUMN status SET DEFAULT 'confirmed';

UPDATE cashflow_entries
    SET status = 'confirmed'
    WHERE status IN ('pending', 'paid');

ALTER TABLE cashflow_entries
    ADD CONSTRAINT cashflow_entries_status_check
        CHECK (status IN ('confirmed', 'overdue'));
