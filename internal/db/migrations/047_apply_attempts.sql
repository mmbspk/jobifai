-- +goose Up
CREATE TABLE IF NOT EXISTS apply_attempts (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL,
    platform     TEXT NOT NULL,
    job_id       TEXT NOT NULL DEFAULT '',
    url          TEXT NOT NULL DEFAULT '',
    role         TEXT NOT NULL DEFAULT '',
    company      TEXT NOT NULL DEFAULT '',
    entry_point  TEXT NOT NULL DEFAULT '',
    outcome      TEXT NOT NULL DEFAULT 'unknown',
    stage        TEXT NOT NULL DEFAULT '',
    reason_code  TEXT NOT NULL DEFAULT '',
    reason_text  TEXT NOT NULL DEFAULT '',
    started_at   DATETIME NOT NULL DEFAULT (datetime('now')),
    finished_at  DATETIME,
    elapsed_ms   INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX idx_apply_attempts_user_id ON apply_attempts(user_id);
CREATE INDEX idx_apply_attempts_job_id ON apply_attempts(job_id);
CREATE INDEX idx_apply_attempts_started_at ON apply_attempts(started_at);
CREATE INDEX idx_apply_attempts_outcome ON apply_attempts(outcome);

-- +goose Down
DROP INDEX IF EXISTS idx_apply_attempts_outcome;
DROP INDEX IF EXISTS idx_apply_attempts_started_at;
DROP INDEX IF EXISTS idx_apply_attempts_job_id;
DROP INDEX IF EXISTS idx_apply_attempts_user_id;
DROP TABLE IF EXISTS apply_attempts;
