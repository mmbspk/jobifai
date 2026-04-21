-- +goose Up
ALTER TABLE jobs_pending_review ADD COLUMN suitability_score INTEGER NOT NULL DEFAULT 0;

-- +goose Down
-- SQLite does not support DROP COLUMN in older versions; no-op rollback
SELECT 1;
