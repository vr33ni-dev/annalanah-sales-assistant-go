-- 1) Rename columns to participant-centric naming
ALTER TABLE stage_participants
  RENAME COLUMN lead_name  TO participant_name;

ALTER TABLE stage_participants
  RENAME COLUMN lead_email TO participant_email;

ALTER TABLE stage_participants
  RENAME COLUMN lead_phone TO participant_phone;

-- 2) Ensure participant always has a name
ALTER TABLE stage_participants
  ALTER COLUMN participant_name SET NOT NULL;

-- 3) Add optional link to leads (client link already exists)
ALTER TABLE stage_participants
  ADD COLUMN linked_lead_id INT REFERENCES leads(id) ON DELETE SET NULL;

-- 4) Helpful indexes
CREATE INDEX IF NOT EXISTS idx_stage_participants_linked_lead_id
  ON stage_participants (linked_lead_id);

-- already exists, but just to be explicit
CREATE INDEX IF NOT EXISTS idx_stage_participants_linked_client_id
  ON stage_participants (linked_client_id);
