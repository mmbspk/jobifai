-- +goose Up
-- +goose StatementBegin

ALTER TABLE users ADD COLUMN email_verified BOOLEAN NOT NULL DEFAULT 0;

-- Google-authenticated accounts are considered pre-verified.
UPDATE users SET email_verified = 1 WHERE google_id IS NOT NULL AND google_id != '';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- SQLite does not support DROP COLUMN before 3.35.0; recreate the table.
CREATE TABLE users_new (
    id            TEXT PRIMARY KEY,
    email         TEXT UNIQUE NOT NULL,
    password_hash TEXT,
    display_name  TEXT NOT NULL DEFAULT '',
    google_id     TEXT UNIQUE,
    avatar_url    TEXT,
    is_admin      BOOLEAN NOT NULL DEFAULT 0,
    verbose_logs  INTEGER NOT NULL DEFAULT 0,
    created_at    DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at    DATETIME NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO users_new SELECT id, email, password_hash, display_name, google_id, avatar_url, is_admin, verbose_logs, created_at, updated_at FROM users;
DROP TABLE users;
ALTER TABLE users_new RENAME TO users;

-- +goose StatementEnd
