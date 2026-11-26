-- +goose Up
CREATE TABLE apply_url_tasks (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    url TEXT NOT NULL,
    market TEXT NOT NULL DEFAULT '',
    force INTEGER NOT NULL DEFAULT 0,
    state TEXT NOT NULL CHECK (state IN ('running','succeeded','failed','cancelled','interrupted')),
    response_json TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    started_at TEXT NOT NULL,
    finished_at TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX apply_url_tasks_active_user ON apply_url_tasks(user_id) WHERE state = 'running';
CREATE INDEX apply_url_tasks_user_recent ON apply_url_tasks(user_id, started_at DESC);

-- +goose Down
DROP TABLE apply_url_tasks;
