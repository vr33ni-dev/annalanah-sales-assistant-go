-- Create a flexible, polymorphic comments table for notes/annotations across entities.
CREATE TABLE comments (
    id SERIAL PRIMARY KEY,
    entity_type TEXT NOT NULL,        -- e.g. 'client', 'sales_process', 'contract', 'lead', ...
    entity_id   INT NOT NULL,         -- id of the referenced row in the target table
    author      TEXT,                 -- optional free-form author (name, email or user id)
    body        TEXT NOT NULL,        -- comment text
    metadata    JSONB,                -- optional extensible metadata (tags, attachments, etc.)
    created_at  TIMESTAMPTZ DEFAULT now(),
    updated_at  TIMESTAMPTZ DEFAULT now()
);

-- Indexes for fast lookups by entity and recent comments
CREATE INDEX IF NOT EXISTS idx_comments_entity ON comments (entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_comments_created_at ON comments (created_at);

-- Trigger: keep `updated_at` fresh on modifications
CREATE OR REPLACE FUNCTION comments_updated_at_trigger()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER comments_set_updated_at
BEFORE UPDATE ON comments
FOR EACH ROW
EXECUTE FUNCTION comments_updated_at_trigger();