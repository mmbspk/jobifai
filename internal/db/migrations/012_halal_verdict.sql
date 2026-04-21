-- +goose Up
ALTER TABLE jobs_skipped ADD COLUMN halal_verdict TEXT;

-- +goose Down
-- SQLite does not support DROP COLUMN in older versions; leave as-is on rollback.
