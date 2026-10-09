-- +goose Up
ALTER TABLE users ADD COLUMN is_tester BOOLEAN NOT NULL DEFAULT 0;

-- +goose Down
