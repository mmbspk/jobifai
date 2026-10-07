-- +goose Up
-- +goose StatementBegin

-- Nullable columns for a pending email-address change.
-- NULL in all three means no pending change.
-- The raw token is never stored; only its SHA-256 hash is persisted here.
ALTER TABLE users ADD COLUMN pending_email            TEXT;
ALTER TABLE users ADD COLUMN email_change_token_hash  TEXT;
ALTER TABLE users ADD COLUMN email_change_expires_at  DATETIME;

CREATE UNIQUE INDEX idx_users_email_change_token ON users(email_change_token_hash)
    WHERE email_change_token_hash IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- SQLite does not support DROP COLUMN before 3.35.0; recreate the table.
CREATE TABLE users_new (
    id              TEXT PRIMARY KEY,
    email           TEXT UNIQUE NOT NULL,
    password_hash   TEXT,
    display_name    TEXT NOT NULL DEFAULT '',
    google_id       TEXT UNIQUE,
    avatar_url      TEXT,
    is_admin        BOOLEAN NOT NULL DEFAULT 0,
    verbose_logs    INTEGER NOT NULL DEFAULT 0,
    email_verified  BOOLEAN NOT NULL DEFAULT 0,
    created_at      DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at      DATETIME NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO users_new
    SELECT id, email, password_hash, display_name, google_id, avatar_url,
           is_admin, verbose_logs, email_verified, created_at, updated_at
    FROM users;
DROP TABLE users;
ALTER TABLE users_new RENAME TO users;

-- +goose StatementEnd
