-- +goose Up
ALTER TABLE usage_totals ADD COLUMN model TEXT NOT NULL DEFAULT '';
CREATE TABLE usage_totals_new (
  user_id       TEXT      NOT NULL,
  model         TEXT      NOT NULL DEFAULT '',
  input_tokens  INTEGER   NOT NULL DEFAULT 0,
  output_tokens INTEGER   NOT NULL DEFAULT 0,
  calls         INTEGER   NOT NULL DEFAULT 0,
  updated_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (user_id, model)
);
INSERT INTO usage_totals_new SELECT user_id, model, input_tokens, output_tokens, calls, updated_at FROM usage_totals;
DROP TABLE usage_totals;
ALTER TABLE usage_totals_new RENAME TO usage_totals;

-- +goose Down
CREATE TABLE usage_totals_old (
  user_id       TEXT      PRIMARY KEY,
  input_tokens  INTEGER   NOT NULL DEFAULT 0,
  output_tokens INTEGER   NOT NULL DEFAULT 0,
  calls         INTEGER   NOT NULL DEFAULT 0,
  updated_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO usage_totals_old
  SELECT user_id, SUM(input_tokens), SUM(output_tokens), SUM(calls), MAX(updated_at)
  FROM usage_totals GROUP BY user_id;
DROP TABLE usage_totals;
ALTER TABLE usage_totals_old RENAME TO usage_totals;
