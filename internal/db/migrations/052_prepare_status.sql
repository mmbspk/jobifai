-- +goose Up
ALTER TABLE jobs_pending_review ADD COLUMN prepare_status TEXT NOT NULL DEFAULT '';
ALTER TABLE jobs_pending_review ADD COLUMN prepare_started_at DATETIME;
ALTER TABLE jobs_pending_review ADD COLUMN prepare_finished_at DATETIME;

-- +goose Down
