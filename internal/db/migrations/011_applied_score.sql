-- +goose Up
ALTER TABLE jobs_applied ADD COLUMN suitability_score INTEGER NOT NULL DEFAULT 0;

-- +goose Down
-- SQLite does not support DROP COLUMN in older versions; leave as-is on rollback.
