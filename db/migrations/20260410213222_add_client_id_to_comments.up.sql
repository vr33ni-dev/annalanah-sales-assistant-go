ALTER TABLE comments
    ADD COLUMN client_id INT REFERENCES clients(id) ON DELETE CASCADE;

CREATE INDEX IF NOT EXISTS idx_comments_client_id ON comments (client_id);
