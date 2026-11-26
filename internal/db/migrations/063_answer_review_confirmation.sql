-- +goose Up
ALTER TABLE application_answer_reviews ADD COLUMN confirmed_answer TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE application_answer_reviews DROP COLUMN confirmed_answer;
