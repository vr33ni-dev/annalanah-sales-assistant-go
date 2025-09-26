-- ======================
-- Seed stages (marketing events)
-- ======================
INSERT INTO stages (name, date, ad_budget, registrations, participants)
VALUES 
('Facebook Ads September', '2025-09-01', 2000, 50, 30);

-- ======================
-- Seed clients
-- ======================
-- Organic client (no source_stage_id)
INSERT INTO clients (name, email, phone, source, source_stage_id)
VALUES 
('Anna Schmidt', 'anna@example.com', '123456', 'organic', NULL);

-- Paid client (linked to stage 1)
INSERT INTO clients (name, email, phone, source, source_stage_id)
VALUES 
('Max Müller', 'max@example.com', '987654', 'paid', 1);

-- ======================
-- Seed sales process
-- ======================
INSERT INTO sales_process (client_id, stage, zweitgespraech_date, zweitgespraech_result, abschluss, revenue, stage_id)
VALUES 
-- Organic client Anna (had zweitgespräch, but no contract yet)
(1, 'zweitgespraech', '2025-09-10', true, false, NULL, NULL),

-- Paid client Max (closed contract, linked to stage 1)
(2, 'abschluss', '2025-09-05', true, true, 6000, 1);

-- ======================
-- Seed contract for Max
-- ======================
INSERT INTO contracts (client_id, sales_process_id, start_date, duration_months, revenue_total, payment_frequency)
VALUES (2, 2, '2025-09-15', 6, 6000, 'bi-monthly');

-- ======================
-- Seed cashflow entries (manual for now)
-- ======================
INSERT INTO cashflow_entries (contract_id, due_date, amount, status)
VALUES
(1, '2025-11-15', 2000, 'pending'),
(1, '2026-01-15', 2000, 'pending'),
(1, '2026-03-15', 2000, 'pending');

-- ======================
-- Seed attribution
-- ======================
INSERT INTO stage_client_assignments (client_id, stage_id)
VALUES (2, 1);

-- ======================
-- Seed participants (optional leads + linked clients)
-- ======================
-- Case A: Lead-only participant (no client yet)
INSERT INTO stage_participants (stage_id, lead_name, lead_email, attended)
VALUES (1, 'Laura Beispiel', 'laura@example.com', true);

-- Case B: Participant linked to existing client (Anna, client_id=1)
INSERT INTO stage_participants (stage_id, linked_client_id, attended)
VALUES (1, 1, true);

-- Case C: Participant linked to existing client (Max, client_id=2)
INSERT INTO stage_participants (stage_id, linked_client_id, attended)
VALUES (1, 2, true);
