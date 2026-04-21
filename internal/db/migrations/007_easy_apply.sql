-- +goose Up
ALTER TABLE jobs_pending_review ADD COLUMN easy_apply INTEGER NOT NULL DEFAULT 1;

-- +goose Down
-- SQLite does not support DROP COLUMN; leave as-is
