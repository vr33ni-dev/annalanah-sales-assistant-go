ALTER TABLE leads
ADD CONSTRAINT unique_lead_email UNIQUE (email);
