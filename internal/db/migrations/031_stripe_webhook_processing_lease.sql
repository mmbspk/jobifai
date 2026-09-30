-- +goose Up
ALTER TABLE stripe_webhook_events ADD COLUMN processing_started_at TIMESTAMP;

-- +goose Down
ALTER TABLE stripe_webhook_events DROP COLUMN processing_started_at;
