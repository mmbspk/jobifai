-- +goose Up
ALTER TABLE apply_attempts ADD COLUMN external_identity_json TEXT NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE apply_attempts DROP COLUMN external_identity_json;
