-- +goose Up
CREATE TABLE IF NOT EXISTS stripe_webhook_events (
  event_id                 TEXT PRIMARY KEY,
  event_type               TEXT NOT NULL,
  stripe_created_at        TIMESTAMP,
  received_at              TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  processed_at             TIMESTAMP,
  status                   TEXT NOT NULL DEFAULT 'received',
  attempt_count            INTEGER NOT NULL DEFAULT 1,
  stripe_customer_id       TEXT,
  stripe_subscription_id   TEXT,
  checkout_session_id      TEXT,
  user_id                  TEXT,
  error_code               TEXT,
  error_message            TEXT,
  metadata_json            TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_stripe_webhook_events_status ON stripe_webhook_events(status, received_at DESC);
CREATE INDEX IF NOT EXISTS idx_stripe_webhook_events_user ON stripe_webhook_events(user_id, received_at DESC);
CREATE INDEX IF NOT EXISTS idx_stripe_webhook_events_type ON stripe_webhook_events(event_type, received_at DESC);

CREATE TABLE IF NOT EXISTS stripe_credit_grants (
  id                   TEXT PRIMARY KEY,
  grant_type           TEXT NOT NULL,
  stripe_event_id      TEXT NOT NULL UNIQUE,
  checkout_session_id  TEXT UNIQUE,
  user_id              TEXT NOT NULL,
  credits              INTEGER NOT NULL,
  created_at           TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_stripe_credit_grants_user ON stripe_credit_grants(user_id, created_at DESC);

ALTER TABLE user_quota ADD COLUMN stripe_subscription_status TEXT NOT NULL DEFAULT '';
ALTER TABLE user_quota ADD COLUMN cancel_at_period_end INTEGER NOT NULL DEFAULT 0;
ALTER TABLE user_quota ADD COLUMN stripe_price_id TEXT NOT NULL DEFAULT '';
ALTER TABLE user_quota ADD COLUMN last_stripe_event_id TEXT NOT NULL DEFAULT '';
ALTER TABLE user_quota ADD COLUMN subscription_updated_at TIMESTAMP;

-- +goose Down
ALTER TABLE user_quota DROP COLUMN subscription_updated_at;
ALTER TABLE user_quota DROP COLUMN last_stripe_event_id;
ALTER TABLE user_quota DROP COLUMN stripe_price_id;
ALTER TABLE user_quota DROP COLUMN cancel_at_period_end;
ALTER TABLE user_quota DROP COLUMN stripe_subscription_status;
DROP TABLE IF EXISTS stripe_credit_grants;
DROP TABLE IF EXISTS stripe_webhook_events;
