-- +goose Up
ALTER TABLE jobs_pending_review ADD COLUMN location TEXT NOT NULL DEFAULT '';

-- +goose Down
-- SQLite does not support DROP COLUMN in older versions; leave as-is
