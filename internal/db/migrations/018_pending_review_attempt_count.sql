-- +goose Up
ALTER TABLE jobs_pending_review ADD COLUMN attempt_count INTEGER NOT NULL DEFAULT 0;

-- +goose Down
