-- +goose Up
ALTER TABLE user_quota ADD COLUMN trial_ends_at TIMESTAMP;
ALTER TABLE user_quota ADD COLUMN topup_credits_remaining INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE user_quota DROP COLUMN topup_credits_remaining;
ALTER TABLE user_quota DROP COLUMN trial_ends_at;
