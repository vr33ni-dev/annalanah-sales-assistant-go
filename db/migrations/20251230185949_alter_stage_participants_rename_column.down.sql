-- 1) Drop index for linked_lead_id (if created)
DROP INDEX IF EXISTS idx_stage_participants_linked_lead_id;

-- 2) Drop linked_lead_id column
ALTER TABLE stage_participants
  DROP COLUMN IF EXISTS linked_lead_id;

-- 3) Remove NOT NULL constraint from participant_name
ALTER TABLE stage_participants
  ALTER COLUMN participant_name DROP NOT NULL;

-- 4) Rename participant-centric columns back to lead-centric naming
ALTER TABLE stage_participants
  RENAME COLUMN participant_name  TO lead_name;

ALTER TABLE stage_participants
  RENAME COLUMN participant_email TO lead_email;

ALTER TABLE stage_participants
  RENAME COLUMN participant_phone TO lead_phone;
