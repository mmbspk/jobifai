-- +goose Up
-- Hide removed library documents while retaining immutable versions referenced
-- by submitted applications and frozen review packs.
ALTER TABLE user_documents ADD COLUMN deleted_at TEXT;
CREATE INDEX idx_user_documents_active ON user_documents(user_id, updated_at) WHERE deleted_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_user_documents_active;
ALTER TABLE user_documents DROP COLUMN deleted_at;
