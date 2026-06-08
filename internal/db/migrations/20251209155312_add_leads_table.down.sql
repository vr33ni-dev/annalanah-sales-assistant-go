-- Rollback: remove leads table
-- Use CASCADE to clean up any FKs (e.g. sales_process.lead_id) if present
DROP TABLE IF EXISTS leads CASCADE;