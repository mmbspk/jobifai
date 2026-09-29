-- +goose Up
ALTER TABLE model_eval_runs ADD COLUMN baseline_spec_json TEXT NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE model_eval_runs DROP COLUMN baseline_spec_json;
