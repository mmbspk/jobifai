-- +goose Up
ALTER TABLE document_render_artifacts ADD COLUMN evicted_at TEXT;

-- +goose Down
ALTER TABLE document_render_artifacts DROP COLUMN evicted_at;
