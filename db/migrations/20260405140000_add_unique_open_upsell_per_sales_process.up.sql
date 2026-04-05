-- Prevent concurrent requests from creating duplicate open (pending) upsells
-- for the same sales process. Only one upsell with a NULL upsell_result is
-- allowed per sales_process_id at any time.
CREATE UNIQUE INDEX IF NOT EXISTS idx_contract_upsells_one_open_per_sp
    ON contract_upsells (sales_process_id)
    WHERE upsell_result IS NULL;
