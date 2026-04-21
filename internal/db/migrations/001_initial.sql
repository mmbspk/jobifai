-- +goose Up
-- +goose StatementBegin

CREATE TABLE IF NOT EXISTS settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS secrets (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL  -- AES-GCM encrypted, base64-encoded
);

CREATE TABLE IF NOT EXISTS platform_sessions (
    platform    TEXT PRIMARY KEY,
    cookies_json TEXT NOT NULL,  -- JSON array of cookie objects, encrypted
    login_method TEXT NOT NULL,
    created_at  DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at  DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS jobs_applied (
    id               TEXT PRIMARY KEY,
    platform         TEXT NOT NULL,
    company          TEXT NOT NULL,
    role             TEXT NOT NULL,
    location         TEXT,
    link             TEXT NOT NULL,
    resume_path      TEXT,
    cover_letter_path TEXT,
    applied_at       DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS jobs_skipped (
    id                   TEXT PRIMARY KEY,
    platform             TEXT NOT NULL,
    company              TEXT NOT NULL,
    role                 TEXT NOT NULL,
    location             TEXT,
    link                 TEXT NOT NULL,
    skip_reason          TEXT NOT NULL,
    suitability_score    INTEGER,
    suitability_reasoning TEXT,
    viewed_at            DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS jobs_pending_review (
    job_id            TEXT PRIMARY KEY,
    company           TEXT NOT NULL,
    role              TEXT NOT NULL,
    platform          TEXT NOT NULL,
    resume_path       TEXT,
    cover_letter_path TEXT,
    created_at        DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_jobs_applied_platform   ON jobs_applied(platform);
CREATE INDEX IF NOT EXISTS idx_jobs_applied_applied_at ON jobs_applied(applied_at DESC);
CREATE INDEX IF NOT EXISTS idx_jobs_skipped_platform   ON jobs_skipped(platform);
CREATE INDEX IF NOT EXISTS idx_jobs_skipped_reason     ON jobs_skipped(skip_reason);

-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS jobs_pending_review;
DROP TABLE IF EXISTS jobs_skipped;
DROP TABLE IF EXISTS jobs_applied;
DROP TABLE IF EXISTS platform_sessions;
DROP TABLE IF EXISTS secrets;
DROP TABLE IF EXISTS settings;
