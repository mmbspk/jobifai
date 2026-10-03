-- +goose Up
ALTER TABLE jobs_pending_review ADD COLUMN resume_content_version_id TEXT NOT NULL DEFAULT '';
ALTER TABLE jobs_pending_review ADD COLUMN cover_letter_content_version_id TEXT NOT NULL DEFAULT '';
ALTER TABLE jobs_pending_review ADD COLUMN document_refs_json TEXT NOT NULL DEFAULT '';

ALTER TABLE jobs_applied ADD COLUMN resume_content_version_id TEXT NOT NULL DEFAULT '';
ALTER TABLE jobs_applied ADD COLUMN cover_letter_content_version_id TEXT NOT NULL DEFAULT '';
ALTER TABLE jobs_applied ADD COLUMN document_refs_json TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE jobs_applied DROP COLUMN document_refs_json;
ALTER TABLE jobs_applied DROP COLUMN cover_letter_content_version_id;
ALTER TABLE jobs_applied DROP COLUMN resume_content_version_id;
ALTER TABLE jobs_pending_review DROP COLUMN document_refs_json;
ALTER TABLE jobs_pending_review DROP COLUMN cover_letter_content_version_id;
ALTER TABLE jobs_pending_review DROP COLUMN resume_content_version_id;
