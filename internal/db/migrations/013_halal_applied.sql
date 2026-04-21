-- +goose Up
ALTER TABLE jobs_applied         ADD COLUMN halal_verdict TEXT;
ALTER TABLE jobs_pending_review  ADD COLUMN halal_verdict TEXT;

-- +goose Down
-- SQLite does not support DROP COLUMN in older versions; leave as-is on rollback.
