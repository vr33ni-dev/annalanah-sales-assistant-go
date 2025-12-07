-- Replace NULL values with a default
UPDATE contract_upsells
SET upsell_result = 'keine_verlaengerung'
WHERE upsell_result IS NULL;

-- Reinstate NOT NULL constraint
ALTER TABLE contract_upsells
ALTER COLUMN upsell_result SET NOT NULL;
