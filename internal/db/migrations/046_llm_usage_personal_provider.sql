-- +goose Up
ALTER TABLE llm_usage_events ADD COLUMN personal_provider BOOLEAN NOT NULL DEFAULT 0;

-- +goose Down
