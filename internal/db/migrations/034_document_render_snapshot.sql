-- +goose Up
ALTER TABLE document_content_versions ADD COLUMN render_snapshot_json TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE document_content_versions DROP COLUMN render_snapshot_json;
