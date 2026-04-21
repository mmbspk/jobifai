-- +goose Up
ALTER TABLE jobs_pending_review ADD COLUMN suitability_reasoning TEXT NOT NULL DEFAULT '';
ALTER TABLE jobs_pending_review ADD COLUMN due_date TEXT NOT NULL DEFAULT '';

-- +goose Down
-- SQLite does not support DROP COLUMN in older versions; leave as-is
