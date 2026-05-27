ALTER TABLE contract_upsells
    UPDATE contract_upsells
    SET upsell_date = CURRENT_DATE
    WHERE upsell_date IS NULL;

ALTER TABLE contract_upsells
    ALTER COLUMN upsell_date SET NOT NULL;
