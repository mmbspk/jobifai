-- +goose Up
-- +goose StatementBegin

-- Seed deployment-wide defaults from the legacy default user scope when present.
INSERT OR IGNORE INTO settings (user_id, key, value)
SELECT '__system__', key, value FROM settings
WHERE user_id = '__default__' AND key = 'general_settings';

INSERT OR IGNORE INTO secrets (user_id, key, value)
SELECT '__system__', key, value FROM secrets
WHERE user_id = '__default__' AND key IN ('llm_api_key', 'proxy_key');

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DELETE FROM settings WHERE user_id = '__system__';
DELETE FROM secrets WHERE user_id = '__system__';

-- +goose StatementEnd
