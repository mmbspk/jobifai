-- +goose Up
ALTER TABLE users ADD COLUMN verbose_logs INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE users DROP COLUMN verbose_logs;
