-- Add nullable lead_id to sales_process to link a lead -> Zweitgespräch
ALTER TABLE sales_process
  ADD COLUMN IF NOT EXISTS lead_id INT REFERENCES leads(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_sales_process_lead_id ON sales_process (lead_id);