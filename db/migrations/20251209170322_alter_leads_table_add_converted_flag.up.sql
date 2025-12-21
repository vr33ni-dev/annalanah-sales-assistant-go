-- Add converted flag + audit fields to leads
ALTER TABLE leads
  ADD COLUMN IF NOT EXISTS converted BOOLEAN DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS converted_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS converted_client_id INT REFERENCES clients(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_leads_converted ON leads (converted);