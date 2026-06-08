-- leads.converted_client_id — correlated subquery in ListClients CTE
-- SELECT l.id FROM leads WHERE l.converted_client_id = c.id
CREATE INDEX IF NOT EXISTS idx_leads_converted_client_id ON leads (converted_client_id);

-- NOTE: sales_process.client_id is already covered by UNIQUE (client_id) constraint.
-- NOTE: idx_comments_entity already created in 20260105204547_create_comments_table.
