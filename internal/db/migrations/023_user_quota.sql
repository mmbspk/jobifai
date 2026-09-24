-- +goose Up
CREATE TABLE IF NOT EXISTS user_quota (
  user_id                  TEXT    PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  plan                     TEXT    NOT NULL DEFAULT 'trial',
  enforcement_enabled      INTEGER NOT NULL DEFAULT 1,
  trial_remaining_micro    INTEGER NOT NULL DEFAULT 0,
  period_allowance_micro   INTEGER NOT NULL DEFAULT 0,
  period_used_micro        INTEGER NOT NULL DEFAULT 0,
  period_start_at          TIMESTAMP,
  period_end_at            TIMESTAMP,
  overage_debt_micro       INTEGER NOT NULL DEFAULT 0,
  stripe_customer_id       TEXT,
  stripe_subscription_id   TEXT,
  created_at               TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at               TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_user_quota_stripe_customer ON user_quota(stripe_customer_id);
CREATE INDEX IF NOT EXISTS idx_user_quota_stripe_sub ON user_quota(stripe_subscription_id);

-- +goose Down
DROP TABLE IF EXISTS user_quota;
