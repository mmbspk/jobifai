-- +goose Up

CREATE TABLE resource_usage_events (
    id                    TEXT    NOT NULL PRIMARY KEY,
    user_id               TEXT    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    resource_type         TEXT    NOT NULL,
    operation_id          TEXT    NOT NULL DEFAULT '',
    idempotency_key       TEXT    NOT NULL DEFAULT '',
    automation_run_id     TEXT    NOT NULL DEFAULT '',
    application_id        TEXT    NOT NULL DEFAULT '',
    job_id                TEXT    NOT NULL DEFAULT '',
    quantity              REAL    NOT NULL DEFAULT 0,
    unit                  TEXT    NOT NULL DEFAULT '',
    duration_ms           INTEGER NOT NULL DEFAULT 0,
    raw_cost_usd_micro    INTEGER NOT NULL DEFAULT 0,
    loaded_cost_usd_micro INTEGER NOT NULL DEFAULT 0,
    credits_burned        INTEGER NOT NULL DEFAULT 0,
    success               INTEGER NOT NULL DEFAULT 1,
    error_code            TEXT    NOT NULL DEFAULT '',
    metadata_json         TEXT    NOT NULL DEFAULT '{}',
    created_at            TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

-- Unique idempotency: prevents duplicate charges for the same logical operation.
-- Partial index excludes empty key rows (diagnostic failures recorded without idempotency).
CREATE UNIQUE INDEX resource_usage_events_idempotency
    ON resource_usage_events(idempotency_key)
    WHERE idempotency_key != '';

CREATE INDEX resource_usage_events_user_date
    ON resource_usage_events(user_id, created_at);

CREATE INDEX resource_usage_events_type_date
    ON resource_usage_events(resource_type, created_at);

CREATE INDEX resource_usage_events_run
    ON resource_usage_events(automation_run_id)
    WHERE automation_run_id != '';

CREATE INDEX resource_usage_events_job
    ON resource_usage_events(job_id)
    WHERE job_id != '';

CREATE INDEX resource_usage_events_app
    ON resource_usage_events(application_id)
    WHERE application_id != '';

-- +goose Down

DROP TABLE IF EXISTS resource_usage_events;
