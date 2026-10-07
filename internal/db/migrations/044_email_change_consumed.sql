-- +goose Up
-- +goose StatementBegin

-- Records consumed (or replaced) email-change token hashes so that replay
-- of a previously-valid token returns 410 Gone rather than 400 Bad Request.
-- Only the SHA-256 hash is stored — the raw token is never persisted.
CREATE TABLE IF NOT EXISTS consumed_email_change_tokens (
    token_hash  TEXT PRIMARY KEY,
    consumed_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS consumed_email_change_tokens;

-- +goose StatementEnd
