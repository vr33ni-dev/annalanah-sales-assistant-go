-- Ensure app_settings table exists before running this seed

WITH
-- 0) Global tunables
settings_months AS (
  INSERT INTO app_settings (key, value_numeric)
  VALUES ('potential_months', 6)
  ON CONFLICT (key) DO UPDATE SET value_numeric = EXCLUDED.value_numeric
  RETURNING 1
),
settings_avg_rev AS (
  INSERT INTO app_settings (key, value_numeric)
  VALUES ('avg_revenue_per_contract', 600)
  ON CONFLICT (key) DO UPDATE SET value_numeric = EXCLUDED.value_numeric
  RETURNING 1
),

-- Keep most dev-seed dates close to "today" so the UI looks current after reset
seed_dates AS (
  SELECT
    (CURRENT_DATE - INTERVAL '2 months')::date AS anna_contract_start,
    (CURRENT_DATE - INTERVAL '1 month')::date AS max_contract_start,
    ((CURRENT_DATE - INTERVAL '2 months')::date - INTERVAL '7 days')::date AS anna_completed_at,
    ((CURRENT_DATE - INTERVAL '1 month')::date - INTERVAL '7 days')::date AS max_completed_at,
    (CURRENT_DATE + INTERVAL '1 month')::date AS moritz_ext_contract_start,
    -- Keep completion timestamps in the past/present (never future), even if the next contract starts in the future
    (CURRENT_DATE - INTERVAL '5 days')::date AS moritz_completed_at,

    -- Explicit end_date test scenario (keep it close to "today")
    (CURRENT_DATE - INTERVAL '20 days')::date AS explicit_contract_start,
    ((CURRENT_DATE - INTERVAL '20 days')::date + INTERVAL '90 days')::date AS explicit_contract_end,
    ((CURRENT_DATE - INTERVAL '20 days')::date - INTERVAL '7 days')::date AS explicit_completed_at
),

-- 1) Stage (ad campaign)
s AS (
  INSERT INTO stages (name, date, ad_budget, registrations, participants)
  VALUES ('Facebook Ads September', (CURRENT_DATE + INTERVAL '30 days')::date, 2000, 50, 30)
  RETURNING id
),

-- 2c) Client for explicit end_date test
explicit_enddate_client AS (
  INSERT INTO clients (name, email, phone, source, status, completed_at)
  -- Closed deal: completion should be before contract start.
  SELECT
    'Explicit Enddate Client',
    'explicit@enddate.com',
    '555000111',
    'organic',
    'active',
    sd.explicit_completed_at
  FROM seed_dates sd
  RETURNING id
),
anna AS (
  INSERT INTO clients (name, email, phone, source, source_stage_id, status, completed_at)
  SELECT 'Anna Schmidt', 'anna@example.com', '123456', 'organic', NULL, 'active', sd.anna_completed_at
  FROM seed_dates sd
  RETURNING id
),
maxc AS (
  INSERT INTO clients (name, email, phone, source, source_stage_id, status, completed_at)
  SELECT 'Max Müller', 'max@example.com', '987654', 'paid', s.id, 'active', sd.max_completed_at
  FROM s, seed_dates sd
  RETURNING id
),
moritz AS (
  INSERT INTO clients (name, email, phone, source, source_stage_id, status, completed_at)
  -- Moritz represents a completed upsell/extension sale in the seed
  SELECT 'Moritz Mustermann', 'mo@example.com', '912345', 'paid', s.id, 'active', sd.moritz_completed_at
  FROM s, seed_dates sd
  RETURNING id
),
maria AS (
  INSERT INTO clients (name, email, phone, source, source_stage_id, status)
  -- Maria represents a no-show that is treated as lost
  SELECT 'Maria Mustermann', 'ma@example.com', '912345', 'paid', s.id, 'lost'
  FROM s
  RETURNING id
),

-- 2b) Leads (dev seed)
leads_ins AS (
  INSERT INTO leads (name, email, phone, source, source_stage_id)
  SELECT 'Peter Beispiel', 'peter@lead.de', '555987654', 'organic', NULL
  UNION ALL
  SELECT 'Laura Beispiel', 'laura@example.com', '444333333', 'paid', s.id FROM s
  UNION ALL
  SELECT 'Test Lead', 'test@example.com', NULL, 'organic', NULL
  RETURNING id
),

-- optional: a lead already converted and linked to Anna
converted_lead AS (
  INSERT INTO leads (name, email, phone, source, source_stage_id, converted, converted_at, converted_client_id)
  SELECT 'Converted Lead', 'conv@example.com', '111222333', 'organic', NULL, TRUE, now(), (SELECT id FROM anna)
  RETURNING id
),


-- Sales process for explicit end_date client
sp_explicit_enddate AS (
  INSERT INTO sales_process (client_id, stage, follow_up_date, follow_up_result, closed, revenue, created_at)
  -- For closed deals, keep the Abschluss meeting before contract start
  SELECT eec.id,
         'closed',
         (sd.explicit_contract_start - INTERVAL '5 days')::date,
         TRUE,
         TRUE,
         1234,
         ((sd.explicit_contract_start - INTERVAL '6 days')::timestamp)
  FROM explicit_enddate_client eec, seed_dates sd
  RETURNING id, client_id
),
-- Anna: closed/won (Abschluss)
sp_anna AS (
  INSERT INTO sales_process (client_id, stage, follow_up_date, follow_up_result, closed, revenue, stage_id, created_at)
  -- For closed deals, keep the Abschluss meeting before contract start
  SELECT a.id,
         'closed',
         (sd.anna_contract_start - INTERVAL '7 days')::date,
         TRUE,
         TRUE,
         4800,
         NULL,
         ((sd.anna_contract_start - INTERVAL '8 days')::timestamp)
  FROM anna a, seed_dates sd
  RETURNING id, client_id
),
-- Max: closed/won (Abschluss) linked to stage
sp_max AS (
  INSERT INTO sales_process (client_id, stage, follow_up_date, follow_up_result, closed, revenue, stage_id, created_at)
  -- For closed deals, keep the Abschluss meeting before contract start
  SELECT m.id,
         'closed',
         (sd.max_contract_start - INTERVAL '7 days')::date,
         TRUE,
         TRUE,
         6000,
         s.id,
         ((sd.max_contract_start - INTERVAL '8 days')::timestamp)
  FROM maxc m, s, seed_dates sd
  RETURNING id, client_id
),
-- Moritz: follow-up done, not closed (FollowUp)
sp_moritz AS (
  INSERT INTO sales_process (client_id, stage, follow_up_date, follow_up_result, closed, revenue, stage_id, created_at)
  -- For a contract to exist, the sales process must be completed (closed).
  -- Keep completion before contract created_at/start_date.
  SELECT mo.id,
         'closed',
         sd.moritz_completed_at,
         TRUE,
         TRUE,
         12000,
         s.id,
         ((sd.moritz_completed_at - INTERVAL '1 day')::timestamp)
  FROM moritz mo, s, seed_dates sd
  RETURNING id, client_id
),
-- Maria: no-show -> lost
sp_maria AS (
  INSERT INTO sales_process (client_id, stage, follow_up_date, follow_up_result, closed, revenue, stage_id, created_at)
  SELECT ma.id,
         'lost',
         (CURRENT_DATE - INTERVAL '1 day')::date,
         FALSE,
         FALSE,
         NULL,
         s.id,
         (((CURRENT_DATE - INTERVAL '1 day')::date - INTERVAL '1 day')::timestamp)
  FROM maria ma, s
  RETURNING id, client_id
),

-- Contract with explicit end_date (not matching start + duration)
contract_explicit_enddate AS (
  INSERT INTO contracts (client_id, sales_process_id, start_date, end_date, duration_months, revenue_total, payment_frequency, created_at)
  SELECT se.client_id,
         se.id,
         sd.explicit_contract_start,
         sd.explicit_contract_end,
         2,
         1234,
         'monthly',
         (sd.explicit_contract_start - INTERVAL '1 day')::timestamptz
  FROM sp_explicit_enddate se, seed_dates sd
  RETURNING id
),
contract_anna AS (
  INSERT INTO contracts (client_id, sales_process_id, start_date, duration_months, revenue_total, payment_frequency, created_at)
  SELECT sa.client_id,
         sa.id,
         sd.anna_contract_start,
         6,
         4800,
         'monthly',
         (sd.anna_contract_start - INTERVAL '1 day')::timestamptz
  FROM sp_anna sa, seed_dates sd
  RETURNING id
),
contract_max AS (
  INSERT INTO contracts (client_id, sales_process_id, start_date, duration_months, revenue_total, payment_frequency, created_at)
  SELECT sm.client_id,
         sm.id,
         sd.max_contract_start,
         6,
         6000,
         'bi-monthly',
         (sd.max_contract_start - INTERVAL '1 day')::timestamptz
  FROM sp_max sm, seed_dates sd
  RETURNING id
),
contract_moritz AS (
  -- Previous contract: not created from the current follow-up sales process (upsell only links it as "previous")
  INSERT INTO contracts (client_id, sales_process_id, start_date, duration_months, revenue_total, payment_frequency, created_at)
  SELECT mo.id,
         NULL,
         (CURRENT_DATE - INTERVAL '11 months')::date,
         12,
         12000,
         'bi-yearly',
         ((CURRENT_DATE - INTERVAL '11 months')::date - INTERVAL '1 day')::timestamptz
  FROM moritz mo
  RETURNING id
),
contract_moritz_ext AS (
  INSERT INTO contracts (client_id, sales_process_id, start_date, duration_months, revenue_total, payment_frequency, created_at)
  SELECT sm.client_id,
         sm.id,
         sd.moritz_ext_contract_start,
         12,
         12000,
         'bi-yearly',
         (CURRENT_DATE::timestamp)
  FROM sp_moritz sm, seed_dates sd
  RETURNING id
),

-- 5) Cashflow entries (pending payments)
cf_ins AS (
  INSERT INTO cashflow_entries (contract_id, due_date, amount, status)
  SELECT c.id, d.due_date, d.amount, d.status
  FROM (
    SELECT (SELECT id FROM contract_anna) AS contract_id, (CURRENT_DATE + INTERVAL '7 days')::date AS due_date, 800::numeric AS amount, 'pending'::text AS status
    UNION ALL SELECT (SELECT id FROM contract_anna), (CURRENT_DATE + INTERVAL '37 days')::date, 800, 'pending'
    UNION ALL SELECT (SELECT id FROM contract_max),  (CURRENT_DATE + INTERVAL '7 days')::date, 2000, 'pending'
    UNION ALL SELECT (SELECT id FROM contract_max),  (CURRENT_DATE + INTERVAL '75 days')::date, 2000, 'pending'
    UNION ALL SELECT (SELECT id FROM contract_moritz), (CURRENT_DATE + INTERVAL '14 days')::date, 6000, 'pending'
    UNION ALL SELECT (SELECT id FROM contract_moritz), (CURRENT_DATE + INTERVAL '194 days')::date, 6000, 'pending'
  ) d
  JOIN contracts c ON c.id = d.contract_id
  RETURNING 1
),

-- 6) Stage assignments & participants

assign_ins AS (
  INSERT INTO stage_client_assignments (client_id, stage_id)
  SELECT m.id, s.id FROM maxc m, s
  RETURNING 1
),

-- Participant that is ALSO a lead (Laura)
part_lead AS (
  INSERT INTO stage_participants (
    stage_id,
    participant_name,
    participant_email,
    participant_phone,
    linked_lead_id,
    attended
  )
  SELECT
    s.id,
    l.name,
    l.email,
    l.phone,
    l.id,
    TRUE
  FROM s
  JOIN leads l ON l.email = 'laura@example.com'
  RETURNING 1
),

-- Participant linked to client Anna
part_anna AS (
  INSERT INTO stage_participants (
    stage_id,
    participant_name,
    participant_email,
    linked_client_id,
    attended
  )
  SELECT
    s.id,
    c.name,
    c.email,
    c.id,
    TRUE
  FROM s
  JOIN clients c ON c.email = 'anna@example.com'
  RETURNING 1
),

-- Participant linked to client Max
part_max AS (
  INSERT INTO stage_participants (
    stage_id,
    participant_name,
    participant_email,
    linked_client_id,
    attended
  )
  SELECT
    s.id,
    c.name,
    c.email,
    c.id,
    TRUE
  FROM s
  JOIN clients c ON c.email = 'max@example.com'
  RETURNING 1
)


-- Final confirmation
SELECT 'ok';

-- Ensure unique constraint so seed can be re-run idempotently
CREATE UNIQUE INDEX IF NOT EXISTS ux_cashflow_contract_due ON cashflow_entries (contract_id, due_date);

-- Generate full cashflow schedules for dev contracts (idempotent)
-- For the dev clients (Anna, Max, Moritz) derive payment dates from contract.start_date,
-- contract.duration_months and contract.payment_frequency and insert entries.
INSERT INTO cashflow_entries (contract_id, due_date, amount, status)
SELECT c.id,
       (c.start_date + (gs.n * (c.step || ' months')::interval))::date AS due_date,
       CASE WHEN c.payment_frequency = 'one-time' THEN c.revenue_total
            ELSE ROUND((c.revenue_total::numeric / NULLIF(c.duration_months,0)) * c.step, 2)
       END AS amount,
       'pending'
FROM (
  SELECT id, start_date, duration_months, revenue_total, payment_frequency,
         CASE payment_frequency
           WHEN 'monthly' THEN 1
           WHEN 'bi-monthly' THEN 2
           WHEN 'quarterly' THEN 3
           WHEN 'bi-yearly' THEN 6
           WHEN 'one-time' THEN GREATEST(duration_months,1)
           ELSE 1
         END AS step
  FROM contracts
  WHERE client_id IN (
    SELECT id FROM clients WHERE email IN ('anna@example.com','max@example.com','mo@example.com')
  )
) c
CROSS JOIN LATERAL generate_series(0, ((c.duration_months - 1) / c.step)) AS gs(n)
ON CONFLICT (contract_id, due_date) DO NOTHING;

-- Insert a confirmed upsell for Moritz linking previous and new contract (idempotent)
INSERT INTO contract_upsells (sales_process_id, client_id, upsell_date, upsell_result, upsell_revenue, previous_contract_id, new_contract_id)
SELECT sp.id, sp.client_id, (sp.follow_up_date + INTERVAL '1 day')::date, 'verlaengerung', nc.revenue_total, pc.id, nc.id
FROM sales_process sp
JOIN clients cl ON cl.id = sp.client_id
JOIN contracts pc ON pc.client_id = cl.id AND pc.start_date <= CURRENT_DATE
JOIN contracts nc ON nc.client_id = cl.id AND nc.start_date > CURRENT_DATE
WHERE cl.email = 'mo@example.com'
  AND sp.stage IN ('follow_up','closed')
  AND NOT EXISTS (
    SELECT 1 FROM contract_upsells cu WHERE cu.sales_process_id = sp.id AND cu.new_contract_id = nc.id
  );
