DROP INDEX IF EXISTS idx_comments_client_id;

ALTER TABLE comments DROP COLUMN IF EXISTS client_id;
