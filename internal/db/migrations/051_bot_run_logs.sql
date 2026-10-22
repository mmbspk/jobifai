-- +goose Up
-- Per-user "current run" record; survives zero-line runs and server restart.
CREATE TABLE bot_current_run (
  user_id    TEXT NOT NULL PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  run_id     TEXT NOT NULL,
  started_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

-- Log rows ordered by insertion. INTEGER PRIMARY KEY provides ordering among
-- retained rows; values may be reused after deletions, so do not use as a
-- monotonic cursor across delete cycles.
CREATE TABLE bot_run_logs (
  seq        INTEGER PRIMARY KEY,
  id         TEXT NOT NULL UNIQUE,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  run_id     TEXT NOT NULL,
  level      TEXT NOT NULL,
  message    TEXT NOT NULL,
  raw_json   TEXT NOT NULL,
  created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_bot_run_logs_user_run ON bot_run_logs(user_id, run_id);

-- +goose Down
DROP INDEX IF EXISTS idx_bot_run_logs_user_run;
DROP TABLE IF EXISTS bot_run_logs;
DROP TABLE IF EXISTS bot_current_run;
