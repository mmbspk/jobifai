-- +goose Up

-- ── settings ────────────────────────────────────────────────────────────────
-- Rebuild settings to use (user_id, key) composite primary key.
ALTER TABLE settings RENAME TO settings_old;

CREATE TABLE settings (
    user_id TEXT NOT NULL DEFAULT '__default__',
    key     TEXT NOT NULL,
    value   TEXT NOT NULL,
    PRIMARY KEY (user_id, key)
);

INSERT INTO settings (user_id, key, value)
SELECT '__default__', key, value FROM settings_old;

DROP TABLE settings_old;

-- ── secrets ──────────────────────────────────────────────────────────────────
ALTER TABLE secrets RENAME TO secrets_old;

CREATE TABLE secrets (
    user_id TEXT NOT NULL DEFAULT '__default__',
    key     TEXT NOT NULL,
    value   TEXT NOT NULL,   -- AES-GCM encrypted, base64-encoded
    PRIMARY KEY (user_id, key)
);

INSERT INTO secrets (user_id, key, value)
SELECT '__default__', key, value FROM secrets_old;

DROP TABLE secrets_old;

-- ── platform_sessions ────────────────────────────────────────────────────────
ALTER TABLE platform_sessions RENAME TO platform_sessions_old;

CREATE TABLE platform_sessions (
    user_id      TEXT NOT NULL DEFAULT '__default__',
    platform     TEXT NOT NULL,
    cookies_json TEXT NOT NULL,  -- JSON array of cookie objects, encrypted
    login_method TEXT NOT NULL,
    created_at   DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at   DATETIME NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (user_id, platform)
);

INSERT INTO platform_sessions (user_id, platform, cookies_json, login_method, created_at, updated_at)
SELECT '__default__', platform, cookies_json, login_method, created_at, updated_at
FROM platform_sessions_old;

DROP TABLE platform_sessions_old;

-- ── jobs_applied ──────────────────────────────────────────────────────────────
ALTER TABLE jobs_applied ADD COLUMN user_id TEXT NOT NULL DEFAULT '__default__';
CREATE INDEX IF NOT EXISTS idx_jobs_applied_user_id ON jobs_applied(user_id);

-- ── jobs_skipped ──────────────────────────────────────────────────────────────
ALTER TABLE jobs_skipped ADD COLUMN user_id TEXT NOT NULL DEFAULT '__default__';
CREATE INDEX IF NOT EXISTS idx_jobs_skipped_user_id ON jobs_skipped(user_id);

-- ── jobs_pending_review ───────────────────────────────────────────────────────
ALTER TABLE jobs_pending_review ADD COLUMN user_id TEXT NOT NULL DEFAULT '__default__';
CREATE INDEX IF NOT EXISTS idx_jobs_pending_user_id ON jobs_pending_review(user_id);

-- ── jobs_approved_queue ───────────────────────────────────────────────────────
ALTER TABLE jobs_approved_queue ADD COLUMN user_id TEXT NOT NULL DEFAULT '__default__';
CREATE INDEX IF NOT EXISTS idx_jobs_approved_user_id ON jobs_approved_queue(user_id);

-- +goose Down

-- Reverse: settings
ALTER TABLE settings RENAME TO settings_new;
CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
INSERT INTO settings (key, value) SELECT key, value FROM settings_new WHERE user_id = '__default__';
DROP TABLE settings_new;

-- Reverse: secrets
ALTER TABLE secrets RENAME TO secrets_new;
CREATE TABLE secrets (key TEXT PRIMARY KEY, value TEXT NOT NULL);
INSERT INTO secrets (key, value) SELECT key, value FROM secrets_new WHERE user_id = '__default__';
DROP TABLE secrets_new;

-- Reverse: platform_sessions
ALTER TABLE platform_sessions RENAME TO platform_sessions_new;
CREATE TABLE platform_sessions (
    platform     TEXT PRIMARY KEY,
    cookies_json TEXT NOT NULL,
    login_method TEXT NOT NULL,
    created_at   DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at   DATETIME NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO platform_sessions SELECT platform, cookies_json, login_method, created_at, updated_at
FROM platform_sessions_new WHERE user_id = '__default__';
DROP TABLE platform_sessions_new;
