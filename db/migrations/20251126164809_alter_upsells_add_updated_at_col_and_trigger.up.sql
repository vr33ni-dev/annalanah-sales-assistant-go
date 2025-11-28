-- Add updated_at column
ALTER TABLE contract_upsells
ADD COLUMN updated_at TIMESTAMP NOT NULL DEFAULT NOW();

-- Create function if you don't already have it
CREATE OR REPLACE FUNCTION trigger_set_timestamp()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create trigger to update updated_at on every UPDATE
CREATE TRIGGER set_timestamp_contract_upsells
BEFORE UPDATE ON contract_upsells
FOR EACH ROW
EXECUTE FUNCTION trigger_set_timestamp();
