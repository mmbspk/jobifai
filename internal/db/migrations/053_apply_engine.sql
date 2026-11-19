-- +goose Up

-- Apply engine audit trail: records every admin change to ApplyEngineConfig.
CREATE TABLE IF NOT EXISTS apply_engine_audit (
    id          TEXT PRIMARY KEY,
    admin_id    TEXT NOT NULL,
    old_mode    TEXT NOT NULL DEFAULT '',
    new_mode    TEXT NOT NULL DEFAULT '',
    reason      TEXT NOT NULL DEFAULT '',
    config_json TEXT NOT NULL DEFAULT '{}',
    created_at  DATETIME NOT NULL DEFAULT (datetime('now'))
);

-- Engine column on apply_attempts: the mode that was pinned when the attempt started.
-- NULL means the attempt predates this feature (legacy behaviour).
ALTER TABLE apply_attempts ADD COLUMN engine TEXT NOT NULL DEFAULT '';

-- +goose Down
DROP TABLE IF EXISTS apply_engine_audit;
-- SQLite does not support DROP COLUMN; leave engine column in place.
