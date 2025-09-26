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
    source_stage_id INT REFERENCES stages(id),   
    created_at TIMESTAMP DEFAULT now()
);

-- ======================
-- Sales process
-- ======================

CREATE TABLE sales_process (
    id SERIAL PRIMARY KEY,
    client_id INT REFERENCES clients(id),
    stage TEXT CHECK (stage IN ('initial_call','zweitgespraech','abschluss','lost')),
    zweitgespraech_date DATE,
    zweitgespraech_result BOOLEAN,
    abschluss BOOLEAN,
    revenue NUMERIC,
    stage_id INT REFERENCES stages(id),
    created_at TIMESTAMP DEFAULT now(),
    updated_at TIMESTAMP DEFAULT now()
);

ALTER TABLE sales_process
ADD CONSTRAINT unique_client_sales UNIQUE (client_id);


-- ======================
-- Contracts & cashflow
-- ======================

CREATE TABLE contracts (
    id SERIAL PRIMARY KEY,
    client_id INT REFERENCES clients(id),
    sales_process_id INT REFERENCES sales_process(id),
    start_date DATE NOT NULL,
    end_date DATE,
    duration_months INT,
    revenue_total NUMERIC NOT NULL,
    payment_frequency TEXT CHECK (payment_frequency IN ('monthly','bi-monthly','quarterly')),
    created_at TIMESTAMP DEFAULT now()
);

CREATE TABLE cashflow_entries (
    id SERIAL PRIMARY KEY,
    contract_id INT REFERENCES contracts(id),
    due_date DATE NOT NULL,
    amount NUMERIC NOT NULL,
    status TEXT CHECK (status IN ('pending','paid','overdue')) DEFAULT 'pending'
);

-- ======================
-- Stage attribution & attendance
-- ======================

CREATE TABLE stage_client_assignments (
    id SERIAL PRIMARY KEY,
    client_id INT REFERENCES clients(id),
    stage_id INT REFERENCES stages(id),
    assigned_at TIMESTAMP DEFAULT now()
);

CREATE TABLE stage_participants (
    id SERIAL PRIMARY KEY,
    stage_id INT REFERENCES stages(id),
    linked_client_id INT REFERENCES clients(id), -- optional, wenn Lead später ein Client wird
    lead_name  TEXT,
    lead_email TEXT,
    lead_phone TEXT,
    attended BOOLEAN,
    created_at TIMESTAMP DEFAULT now()
);
