-- ensure app_settings table exists in a migration before running this seed

WITH
-- 0) Global tunables
settings_months AS (
  INSERT INTO app_settings (key, value_numeric)
  VALUES ('potential_months', 6)
  ON CONFLICT (key) DO UPDATE SET value_numeric = EXCLUDED.value_numeric
  RETURNING 1
),
settings_flat AS (
  INSERT INTO app_settings (key, value_numeric)
  VALUES ('potential_flat_eur', 600)
  ON CONFLICT (key) DO UPDATE SET value_numeric = EXCLUDED.value_numeric
  RETURNING 1
),

-- 1) Stage
s AS (
  INSERT INTO stages (name, date, ad_budget, registrations, participants)
  VALUES ('Facebook Ads September', '2025-09-01', 2000, 50, 30)
  RETURNING id
),

-- 2) Clients
anna AS (
  INSERT INTO clients (name, email, phone, source, source_stage_id, status)
  VALUES ('Anna Schmidt', 'anna@example.com', '123456', 'organic', NULL, 'active')
  RETURNING id
),
maxc AS (
  INSERT INTO clients (name, email, phone, source, source_stage_id, status)
  SELECT 'Max Müller', 'max@example.com', '987654', 'paid', s.id, 'active'
  FROM s
  RETURNING id
),
moritz AS (
  INSERT INTO clients (name, email, phone, source, source_stage_id, status)
  SELECT 'Moritz Mustermann', 'mo@example.com', '912345', 'paid', s.id, 'follow_up_scheduled'
  FROM s
  RETURNING id
),
maria AS (
  INSERT INTO clients (name, email, phone, source, source_stage_id, status)
  SELECT 'Maria Mustermann', 'ma@example.com', '912345', 'paid', s.id, 'lost'
  FROM s
  RETURNING id
),

-- 3) Sales processes
-- Anna: closed/won so she can have a contract
sp_anna AS (
  INSERT INTO sales_process (client_id, stage, zweitgespraech_date, zweitgespraech_result, abschluss, revenue, stage_id)
  SELECT a.id, 'abschluss', '2025-09-10', TRUE, TRUE, 4800, NULL
  FROM anna a
  RETURNING id, client_id
),
-- Max: closed/won linked to stage
sp_max AS (
  INSERT INTO sales_process (client_id, stage, zweitgespraech_date, zweitgespraech_result, abschluss, revenue, stage_id)
  SELECT m.id, 'abschluss', '2025-09-05', TRUE, TRUE, 6000, s.id
  FROM maxc m, s
  RETURNING id, client_id
),
-- Moritz: post-zweitgespräch but not closed (potential)
sp_moritz AS (
  INSERT INTO sales_process (client_id, stage, zweitgespraech_date, zweitgespraech_result, abschluss, revenue, stage_id)
  SELECT mo.id, 'zweitgespraech', '2025-10-02', TRUE, FALSE, 5400, s.id
  FROM moritz mo, s
  RETURNING id, client_id
),
-- Maria: lost
sp_maria AS (
  INSERT INTO sales_process (client_id, stage, zweitgespraech_date, zweitgespraech_result, abschluss, revenue, stage_id)
  SELECT ma.id, 'lost', '2025-09-20', FALSE, FALSE, NULL, s.id
  FROM maria ma, s
  RETURNING id, client_id
),

-- 4) Contracts for BOTH active clients (Anna + Max)
contract_anna AS (
  INSERT INTO contracts (client_id, sales_process_id, start_date, end_date, duration_months, revenue_total, payment_frequency)
  SELECT sa.client_id, sa.id, '2025-09-20', NULL, 6, 4800, 'monthly'
  FROM sp_anna sa
  RETURNING id
),
contract_max AS (
  INSERT INTO contracts (client_id, sales_process_id, start_date, end_date, duration_months, revenue_total, payment_frequency)
  SELECT sm.client_id, sm.id, '2025-09-15', NULL, 6, 6000, 'bi-monthly'
  FROM sp_max sm
  RETURNING id
),

-- 5) Cashflow entries (some upcoming payments for both)
cf_ins AS (
  INSERT INTO cashflow_entries (contract_id, due_date, amount, status)
  SELECT c.id, d.due_date, d.amount, d.status
  FROM (
    SELECT (SELECT id FROM contract_anna) AS contract_id, '2025-11-15'::date AS due_date, 800::numeric AS amount, 'pending'::text AS status
    UNION ALL SELECT (SELECT id FROM contract_anna), '2025-12-15', 800, 'pending'
    UNION ALL SELECT (SELECT id FROM contract_max),  '2025-11-15', 2000, 'pending'
    UNION ALL SELECT (SELECT id FROM contract_max),  '2026-01-15', 2000, 'pending'
  ) d
  JOIN contracts c ON c.id = d.contract_id
  RETURNING 1
),

-- 6) Stage assignment + participants (all still inside the same WITH)
assign_ins AS (
  INSERT INTO stage_client_assignments (client_id, stage_id)
  SELECT m.id, s.id FROM maxc m, s
  RETURNING 1
),
part_lead AS (
  INSERT INTO stage_participants (stage_id, lead_name, lead_email, attended)
  SELECT s.id, 'Laura Beispiel', 'laura@example.com', TRUE FROM s
  RETURNING 1
),
part_anna AS (
  INSERT INTO stage_participants (stage_id, linked_client_id, attended)
  SELECT s.id, a.id, TRUE FROM s, anna a
  RETURNING 1
),
part_max AS (
  INSERT INTO stage_participants (stage_id, linked_client_id, attended)
  SELECT s.id, m.id, TRUE FROM s, maxc m
  RETURNING 1
)

-- Final select just to end the statement
SELECT 'ok';
