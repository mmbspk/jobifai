-- +goose Up
ALTER TABLE model_eval_results ADD COLUMN budget_charge_usd_micro INTEGER NOT NULL DEFAULT 0;
ALTER TABLE model_eval_results ADD COLUMN output_text TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE model_eval_results DROP COLUMN output_text;
ALTER TABLE model_eval_results DROP COLUMN budget_charge_usd_micro;
