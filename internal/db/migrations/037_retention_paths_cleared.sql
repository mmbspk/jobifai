-- +goose Up
ALTER TABLE jobs_applied ADD COLUMN retention_paths_cleared TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE jobs_applied DROP COLUMN retention_paths_cleared;
