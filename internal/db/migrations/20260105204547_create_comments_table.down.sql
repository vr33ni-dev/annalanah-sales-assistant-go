-- Rollback for add_comments_table
DROP INDEX IF EXISTS idx_comments_created_at;
DROP INDEX IF EXISTS idx_comments_entity;
DROP TABLE IF EXISTS comments;

-- Drop trigger and function
DROP TRIGGER IF EXISTS comments_set_updated_at ON comments;
DROP FUNCTION IF EXISTS comments_updated_at_trigger();