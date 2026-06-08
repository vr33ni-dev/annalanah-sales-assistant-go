-- First, fix any NULLs (required before adding NOT NULL constraint)
UPDATE contract_upsells
SET upsell_result = 'keine_verlaengerung'
WHERE upsell_result IS NULL;

-- Now make column NOT NULL again
ALTER TABLE contract_upsells
    ALTER COLUMN upsell_result SET NOT NULL;
