-- +goose Up
-- +goose StatementBegin

CREATE TABLE IF NOT EXISTS users (
    id            TEXT PRIMARY KEY,           -- UUID v4
    email         TEXT UNIQUE NOT NULL,
    password_hash TEXT,                       -- NULL for Google-only users
    display_name  TEXT NOT NULL DEFAULT '',
    google_id     TEXT UNIQUE,                -- NULL for email-only users
    avatar_url    TEXT,
    created_at    DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at    DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS refresh_tokens (
    id         TEXT PRIMARY KEY,              -- UUID v4
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,          -- SHA-256 of the raw token
    expires_at DATETIME NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user_id ON refresh_tokens(user_id);

-- +goose StatementEnd

-- +goose Down
DROP INDEX IF EXISTS idx_refresh_tokens_user_id;
DROP TABLE IF EXISTS refresh_tokens;
DROP TABLE IF EXISTS users;
