-- Case-insensitive email lookups used in StartSalesProcess deduplication:
--   WHERE LOWER(email) = LOWER($1)
CREATE INDEX IF NOT EXISTS idx_leads_email_lower    ON leads    (LOWER(email));
CREATE INDEX IF NOT EXISTS idx_clients_email_lower  ON clients  (LOWER(email));
