-- ======================
-- Core domain entities
-- ======================

CREATE TABLE stages (
    id SERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    date DATE,
    ad_budget NUMERIC,
    registrations INT,
    participants INT,
    created_at TIMESTAMP DEFAULT now()
);

CREATE TABLE clients (
    id SERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    email TEXT,
    phone TEXT,
    source TEXT CHECK (source IN ('organic', 'paid')),
    source_stage_id INT REFERENCES stages(id) ON DELETE SET NULL,
    created_at TIMESTAMP DEFAULT now(),
    status TEXT CHECK (status IN ('active','follow_up_scheduled','awaiting_response','lost','inactive'))
);

-- ======================
-- Sales process
-- ======================

CREATE TABLE sales_process (
    id SERIAL PRIMARY KEY,
    client_id INT NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
    stage TEXT CHECK (stage IN ('zweitgespraech','abschluss','lost')),
    zweitgespraech_date DATE,
    zweitgespraech_result BOOLEAN,
    abschluss BOOLEAN,
    revenue NUMERIC,
    stage_id INT REFERENCES stages(id) ON DELETE SET NULL,
    created_at TIMESTAMP DEFAULT now(),
    updated_at TIMESTAMP DEFAULT now()
);

-- one (current) sales process per client (as you had)
ALTER TABLE sales_process
  ADD CONSTRAINT unique_client_sales UNIQUE (client_id);

-- Make (id, client_id) addressable for a composite FK from contracts
CREATE UNIQUE INDEX IF NOT EXISTS sales_process_id_client_uidx ON sales_process (id, client_id);

-- Helpful for cashflow "potential" query:
-- stage='zweitgespraech' AND appeared (result=true) AND not closed (abschluss=false)
CREATE INDEX IF NOT EXISTS idx_sales_process_potential_window
  ON sales_process (zweitgespraech_date)
  WHERE stage = 'zweitgespraech'
    AND COALESCE(abschluss, false) = false
    AND zweitgespraech_result = true;

-- ======================
-- Contracts & cashflow
-- ======================

CREATE TABLE contracts (
    id SERIAL PRIMARY KEY,
    client_id INT NOT NULL,
    sales_process_id INT NOT NULL,
    start_date DATE NOT NULL,
    end_date DATE,
    duration_months INT NOT NULL CHECK (duration_months > 0),
    revenue_total NUMERIC NOT NULL CHECK (revenue_total >= 0),
    payment_frequency TEXT NOT NULL CHECK (payment_frequency IN ('monthly','bi-monthly','quarterly')),
    created_at TIMESTAMP DEFAULT now(),
    -- FK: client exists
    CONSTRAINT fk_contracts_client
        FOREIGN KEY (client_id) REFERENCES clients(id) ON DELETE CASCADE,
    -- FK: sales_process belongs to the SAME client (composite reference)
    CONSTRAINT fk_contracts_sales_process_same_client
        FOREIGN KEY (sales_process_id, client_id)
        REFERENCES sales_process(id, client_id)
        ON DELETE RESTRICT
);

-- Enforce: only ONE ACTIVE contract per client (active = end_date IS NULL)
CREATE UNIQUE INDEX IF NOT EXISTS unique_active_contract_per_client
    ON contracts (client_id)
    WHERE end_date IS NULL;

-- Helpful indexes
CREATE INDEX IF NOT EXISTS idx_contracts_sales_process_id ON contracts (sales_process_id);
CREATE INDEX IF NOT EXISTS idx_contracts_client_id       ON contracts (client_id);

CREATE TABLE cashflow_entries (
    id SERIAL PRIMARY KEY,
    contract_id INT NOT NULL REFERENCES contracts(id) ON DELETE CASCADE,
    due_date DATE NOT NULL,
    amount NUMERIC NOT NULL CHECK (amount >= 0),
    status TEXT CHECK (status IN ('pending','paid','overdue')) DEFAULT 'pending'
);

CREATE INDEX IF NOT EXISTS idx_cashflow_contract_id ON cashflow_entries (contract_id);
CREATE INDEX IF NOT EXISTS idx_cashflow_due_date    ON cashflow_entries (due_date);

-- Used by forecast confirmed rollup: range by month + status IN(...)
CREATE INDEX IF NOT EXISTS idx_cashflow_entries_due_status
    ON cashflow_entries (due_date, status);

-- ======================
-- Stage attribution & attendance
-- ======================

CREATE TABLE stage_client_assignments (
    id SERIAL PRIMARY KEY,
    client_id INT NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
    stage_id INT NOT NULL REFERENCES stages(id) ON DELETE CASCADE,
    assigned_at TIMESTAMP DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_stage_client_assignments_client_id ON stage_client_assignments (client_id);
CREATE INDEX IF NOT EXISTS idx_stage_client_assignments_stage_id  ON stage_client_assignments (stage_id);

CREATE TABLE stage_participants (
    id SERIAL PRIMARY KEY,
    stage_id INT NOT NULL REFERENCES stages(id) ON DELETE CASCADE,
    linked_client_id INT REFERENCES clients(id) ON DELETE SET NULL, -- optional link
    lead_name  TEXT,
    lead_email TEXT,
    lead_phone TEXT,
    attended BOOLEAN,
    created_at TIMESTAMP DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_stage_participants_stage_id         ON stage_participants (stage_id);
CREATE INDEX IF NOT EXISTS idx_stage_participants_linked_client_id ON stage_participants (linked_client_id);


-- app_settings: simple key-value store for tunables
CREATE TABLE IF NOT EXISTS app_settings (
  key          text PRIMARY KEY,
  value_numeric numeric,
  value_text    text,
  updated_at    timestamp DEFAULT now()
);
