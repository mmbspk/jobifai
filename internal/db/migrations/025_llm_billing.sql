-- +goose Up
CREATE TABLE IF NOT EXISTS llm_usage_events (
  id                    TEXT PRIMARY KEY,
  created_at            TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  user_id               TEXT NOT NULL,
  task                  TEXT NOT NULL,
  provider              TEXT NOT NULL,
  requested_model       TEXT NOT NULL,
  actual_model          TEXT NOT NULL,
  actual_model_verified INTEGER NOT NULL DEFAULT 0,
  input_tokens          INTEGER NOT NULL DEFAULT 0,
  output_tokens         INTEGER NOT NULL DEFAULT 0,
  cache_write_5m_tokens INTEGER NOT NULL DEFAULT 0,
  cache_write_1h_tokens INTEGER NOT NULL DEFAULT 0,
  cache_read_tokens     INTEGER NOT NULL DEFAULT 0,
  raw_cost_usd_micro    INTEGER NOT NULL DEFAULT 0,
  loaded_cost_usd_micro INTEGER NOT NULL DEFAULT 0,
  credits_burned        INTEGER NOT NULL DEFAULT 0,
  latency_ms            INTEGER NOT NULL DEFAULT 0,
  success               INTEGER NOT NULL DEFAULT 1,
  error_code            TEXT,
  correlation_id        TEXT NOT NULL DEFAULT '',
  idempotency_key       TEXT UNIQUE,
  job_id                TEXT,
  application_id        TEXT,
  automation_run_id     TEXT,
  attempt               INTEGER NOT NULL DEFAULT 1,
  pricing_source        TEXT NOT NULL DEFAULT '',
  pricing_version       TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_llm_usage_user_created ON llm_usage_events(user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_llm_usage_task_created ON llm_usage_events(task, created_at);
CREATE INDEX IF NOT EXISTS idx_llm_usage_model_created ON llm_usage_events(actual_model, created_at);
CREATE INDEX IF NOT EXISTS idx_llm_usage_job_id ON llm_usage_events(job_id);
CREATE INDEX IF NOT EXISTS idx_llm_usage_run_id ON llm_usage_events(automation_run_id);

CREATE TABLE IF NOT EXISTS model_catalog_meta (
  id              INTEGER PRIMARY KEY CHECK (id = 1),
  source          TEXT NOT NULL DEFAULT 'builtin',
  last_refresh_at TIMESTAMP,
  last_error      TEXT,
  pricing_version TEXT NOT NULL DEFAULT '',
  updated_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT OR IGNORE INTO model_catalog_meta (id, source, pricing_version) VALUES (1, 'builtin', '2026-09-29');

CREATE TABLE IF NOT EXISTS task_model_policies (
  task              TEXT PRIMARY KEY,
  state             TEXT NOT NULL DEFAULT 'current',
  provider          TEXT NOT NULL DEFAULT '',
  model             TEXT NOT NULL DEFAULT '',
  fallback_models   TEXT NOT NULL DEFAULT '[]',
  mode              TEXT NOT NULL DEFAULT 'pinned',
  max_tokens        INTEGER NOT NULL DEFAULT 0,
  approved_at       TIMESTAMP,
  approved_by       TEXT,
  eval_run_id       TEXT,
  previous_json     TEXT,
  updated_at        TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS model_policy_audit (
  id            TEXT PRIMARY KEY,
  task          TEXT NOT NULL,
  changed_by    TEXT NOT NULL,
  previous_json TEXT NOT NULL,
  new_json      TEXT NOT NULL,
  eval_run_id   TEXT,
  reason        TEXT,
  created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_model_policy_audit_task ON model_policy_audit(task, created_at);

CREATE TABLE IF NOT EXISTS model_eval_runs (
  id               TEXT PRIMARY KEY,
  task             TEXT NOT NULL,
  created_at       TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  baseline_model   TEXT NOT NULL,
  candidate_models TEXT NOT NULL,
  dataset_version  TEXT NOT NULL DEFAULT '',
  status           TEXT NOT NULL DEFAULT 'pending'
);

CREATE TABLE IF NOT EXISTS model_eval_results (
  id                TEXT PRIMARY KEY,
  eval_run_id       TEXT NOT NULL REFERENCES model_eval_runs(id) ON DELETE CASCADE,
  case_id           TEXT NOT NULL,
  model             TEXT NOT NULL,
  success           INTEGER NOT NULL DEFAULT 0,
  score             REAL,
  metric_json       TEXT NOT NULL DEFAULT '{}',
  input_tokens      INTEGER NOT NULL DEFAULT 0,
  output_tokens     INTEGER NOT NULL DEFAULT 0,
  raw_cost_usd_micro INTEGER NOT NULL DEFAULT 0,
  latency_ms        INTEGER NOT NULL DEFAULT 0,
  validation_errors TEXT NOT NULL DEFAULT '[]'
);

CREATE INDEX IF NOT EXISTS idx_model_eval_results_run ON model_eval_results(eval_run_id);

-- +goose Down
DROP TABLE IF EXISTS model_eval_results;
DROP TABLE IF EXISTS model_eval_runs;
DROP TABLE IF EXISTS model_policy_audit;
DROP TABLE IF EXISTS task_model_policies;
DROP TABLE IF EXISTS model_catalog_meta;
DROP TABLE IF EXISTS llm_usage_events;
