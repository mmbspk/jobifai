-- +goose Up
ALTER TABLE user_quota ADD COLUMN last_stripe_state_event_created_at INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE user_quota DROP COLUMN last_stripe_state_event_created_at;
