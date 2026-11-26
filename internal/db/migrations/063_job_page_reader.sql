-- +goose Up
-- +goose StatementBegin
CREATE TABLE job_page_recipes (
 id TEXT PRIMARY KEY,
 platform TEXT NOT NULL,
 page_type TEXT NOT NULL,
 origin TEXT NOT NULL,
 snapshot_version INTEGER NOT NULL,
 layout_signature TEXT NOT NULL,
 definition_json TEXT NOT NULL,
 definition_hash TEXT NOT NULL,
 verified_at_ns INTEGER NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('active','invalidated','retired')),
 state_reason TEXT NOT NULL DEFAULT '',
 updated_at_ns INTEGER NOT NULL
);
CREATE INDEX job_page_recipes_scope ON job_page_recipes(platform,page_type,origin,snapshot_version,layout_signature,state);
CREATE TABLE job_page_config (
 singleton INTEGER PRIMARY KEY CHECK(singleton=1),
 config_json TEXT NOT NULL,
 revision TEXT NOT NULL,
 updated_at_ns INTEGER NOT NULL
);
CREATE TABLE job_page_admin_audit (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 actor_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 action TEXT NOT NULL,
 subject_id TEXT NOT NULL,
 revision TEXT NOT NULL DEFAULT '',
 created_at_ns INTEGER NOT NULL
);
CREATE TABLE job_page_read_events (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 run_id TEXT NOT NULL DEFAULT '',
 attempt_id TEXT NOT NULL DEFAULT '',
 record_id TEXT NOT NULL DEFAULT '',
 platform TEXT NOT NULL,
 origin TEXT NOT NULL,
 source TEXT NOT NULL DEFAULT '',
 failure_code TEXT NOT NULL DEFAULT '',
 recipe_id TEXT NOT NULL DEFAULT '',
 metrics_json TEXT NOT NULL,
 created_at_ns INTEGER NOT NULL
);
CREATE INDEX job_page_read_events_owner ON job_page_read_events(user_id,created_at_ns);
CREATE TABLE job_page_private_captures (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 read_event_id INTEGER NOT NULL REFERENCES job_page_read_events(id) ON DELETE CASCADE,
 snapshot_json BLOB NOT NULL,
 screenshot BLOB NOT NULL,
 created_at_ns INTEGER NOT NULL,
 expires_at_ns INTEGER NOT NULL
);
CREATE INDEX job_page_private_captures_owner ON job_page_private_captures(user_id,created_at_ns);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE job_page_private_captures;
DROP TABLE job_page_read_events;
DROP TABLE job_page_admin_audit;
DROP TABLE job_page_config;
DROP TABLE job_page_recipes;
-- +goose StatementEnd
