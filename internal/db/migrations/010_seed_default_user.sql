-- +goose Up
-- +goose StatementBegin

-- Seed a default admin user so the app is usable immediately after first run.
-- Credentials: admin@jobifai.local / jobifai2024!
-- The existing data that was stored under '__default__' is re-owned by this user.
INSERT OR IGNORE INTO users (id, email, password_hash, display_name, created_at, updated_at)
VALUES (
    '__default__',
    'admin@jobifai.local',
    '$2a$12$eQt.XGAQFdHIwCoXtZEVFOJWTraD9aHoQbiR2WL/9G.7hnmUNBWMa',
    'Admin',
    datetime('now'),
    datetime('now')
);

-- +goose StatementEnd

-- +goose Down
DELETE FROM users WHERE id = '__default__';
