-- 1) Create upsell table
CREATE TABLE contract_upsells (
    id SERIAL PRIMARY KEY,
    sales_process_id INT NOT NULL REFERENCES sales_process(id) ON DELETE CASCADE,
    client_id INT NOT NULL REFERENCES clients(id) ON DELETE CASCADE,

    upsell_date DATE NOT NULL,
    upsell_result TEXT NOT NULL CHECK (upsell_result IN ('verlaengerung','keine_verlaengerung')),
    upsell_revenue NUMERIC CHECK (upsell_revenue >= 0),

    previous_contract_id INT REFERENCES contracts(id) ON DELETE SET NULL,
    new_contract_id INT REFERENCES contracts(id) ON DELETE SET NULL,

    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- 2) Indexes to speed up analytics / reporting
CREATE INDEX idx_contract_upsells_sales_process
    ON contract_upsells (sales_process_id);

CREATE INDEX idx_contract_upsells_client
    ON contract_upsells (client_id);

CREATE INDEX idx_contract_upsells_result
    ON contract_upsells (upsell_result);

CREATE INDEX idx_contract_upsells_previous_contract
    ON contract_upsells (previous_contract_id);

CREATE INDEX idx_contract_upsells_new_contract
    ON contract_upsells (new_contract_id);

-- Optional for faster date-based reporting (monthly renewal rate, etc.)
CREATE INDEX idx_contract_upsells_date
    ON contract_upsells (upsell_date);
