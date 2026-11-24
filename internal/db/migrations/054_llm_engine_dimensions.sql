-- +goose Up
ALTER TABLE llm_usage_events ADD COLUMN engine TEXT NOT NULL DEFAULT '';
ALTER TABLE llm_usage_events ADD COLUMN apply_attempt_id TEXT NOT NULL DEFAULT '';
ALTER TABLE llm_usage_events ADD COLUMN platform TEXT NOT NULL DEFAULT '';

-- +goose Down
-- SQLite does not support DROP COLUMN on older versions; leave columns in place.
-- To roll back: recreate the table without these columns.
