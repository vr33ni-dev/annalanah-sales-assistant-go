-- Prevent duplicate contracts from double-clicks: one contract per
-- sales process per start date. The partial WHERE clause allows multiple
-- contracts per client from different sales processes or manual entries.
CREATE UNIQUE INDEX IF NOT EXISTS ux_contracts_sp_start
    ON contracts (sales_process_id, start_date)
    WHERE sales_process_id IS NOT NULL;
