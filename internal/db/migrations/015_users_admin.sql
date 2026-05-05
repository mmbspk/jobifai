-- +goose Up
-- +goose StatementBegin

ALTER TABLE users ADD COLUMN is_admin BOOLEAN NOT NULL DEFAULT 0;

-- The seeded default user is the built-in admin account.
UPDATE users SET is_admin = 1 WHERE id = '__default__';

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
    created_at    DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at    DATETIME NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO users_new SELECT id, email, password_hash, display_name, google_id, avatar_url, created_at, updated_at FROM users;
DROP TABLE users;
ALTER TABLE users_new RENAME TO users;

-- +goose StatementEnd
