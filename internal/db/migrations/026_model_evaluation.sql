-- +goose Up
ALTER TABLE task_model_policies ADD COLUMN effort TEXT NOT NULL DEFAULT '';
ALTER TABLE task_model_policies ADD COLUMN max_cost_usd REAL NOT NULL DEFAULT 0;
ALTER TABLE task_model_policies ADD COLUMN timeout_sec INTEGER NOT NULL DEFAULT 0;

ALTER TABLE model_eval_runs ADD COLUMN started_at TIMESTAMP;
ALTER TABLE model_eval_runs ADD COLUMN completed_at TIMESTAMP;
ALTER TABLE model_eval_runs ADD COLUMN baseline_provider TEXT NOT NULL DEFAULT '';
ALTER TABLE model_eval_runs ADD COLUMN baseline_effort TEXT NOT NULL DEFAULT '';
ALTER TABLE model_eval_runs ADD COLUMN dataset_name TEXT NOT NULL DEFAULT '';
ALTER TABLE model_eval_runs ADD COLUMN dataset_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE model_eval_runs ADD COLUMN candidate_spec_json TEXT NOT NULL DEFAULT '[]';
ALTER TABLE model_eval_runs ADD COLUMN judge_spec_json TEXT NOT NULL DEFAULT '{}';
ALTER TABLE model_eval_runs ADD COLUMN max_budget_usd_micro INTEGER NOT NULL DEFAULT 0;
ALTER TABLE model_eval_runs ADD COLUMN actual_cost_usd_micro INTEGER NOT NULL DEFAULT 0;
ALTER TABLE model_eval_runs ADD COLUMN summary_json TEXT NOT NULL DEFAULT '{}';
ALTER TABLE model_eval_runs ADD COLUMN error_message TEXT NOT NULL DEFAULT '';
ALTER TABLE model_eval_runs ADD COLUMN initiated_by TEXT NOT NULL DEFAULT '';

ALTER TABLE model_eval_results ADD COLUMN provider TEXT NOT NULL DEFAULT '';
ALTER TABLE model_eval_results ADD COLUMN requested_model TEXT NOT NULL DEFAULT '';
ALTER TABLE model_eval_results ADD COLUMN actual_model TEXT NOT NULL DEFAULT '';
ALTER TABLE model_eval_results ADD COLUMN effort TEXT NOT NULL DEFAULT '';
ALTER TABLE model_eval_results ADD COLUMN repetition INTEGER NOT NULL DEFAULT 1;
ALTER TABLE model_eval_results ADD COLUMN deterministic_score REAL;
ALTER TABLE model_eval_results ADD COLUMN judge_score REAL;
ALTER TABLE model_eval_results ADD COLUMN actual_model_verified INTEGER NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS model_catalog_candidates (
  id                    TEXT PRIMARY KEY,
  provider              TEXT NOT NULL,
  model                 TEXT NOT NULL,
  canonical_model       TEXT NOT NULL DEFAULT '',
  source                TEXT NOT NULL DEFAULT '',
  first_seen_at         TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  last_seen_at          TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  input_price           REAL,
  output_price          REAL,
  cache_read_price      REAL,
  cache_write_price     REAL,
  context_window        INTEGER,
  max_output            INTEGER,
  vision_support        INTEGER NOT NULL DEFAULT 0,
  structured_output     INTEGER NOT NULL DEFAULT 0,
  reasoning_effort      INTEGER NOT NULL DEFAULT 0,
  active                INTEGER NOT NULL DEFAULT 1,
  deprecated            INTEGER NOT NULL DEFAULT 0,
  raw_metadata_json     TEXT NOT NULL DEFAULT '{}',
  discovery_state       TEXT NOT NULL DEFAULT 'new',
  catalog_diff_json     TEXT NOT NULL DEFAULT '{}',
  UNIQUE(provider, model, source)
);

CREATE INDEX IF NOT EXISTS idx_model_catalog_candidates_state ON model_catalog_candidates(discovery_state, last_seen_at);

CREATE TABLE IF NOT EXISTS model_eval_recommendations (
  id                    TEXT PRIMARY KEY,
  eval_run_id           TEXT NOT NULL REFERENCES model_eval_runs(id) ON DELETE CASCADE,
  task                  TEXT NOT NULL,
  outcome               TEXT NOT NULL,
  baseline_json         TEXT NOT NULL,
  candidate_json        TEXT NOT NULL,
  metrics_json          TEXT NOT NULL DEFAULT '{}',
  reason                TEXT NOT NULL DEFAULT '',
  deployable            INTEGER NOT NULL DEFAULT 0,
  created_at            TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_model_eval_recommendations_task ON model_eval_recommendations(task, created_at);

-- +goose Down
DROP TABLE IF EXISTS model_eval_recommendations;
DROP TABLE IF EXISTS model_catalog_candidates;
-- SQLite cannot drop columns added via ALTER; downgrade is best-effort no-op for column adds.
