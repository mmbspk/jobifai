-- +goose Up
CREATE TABLE user_documents (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('resume', 'cover_letter', 'original_upload')),
    title TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_user_documents_user_kind ON user_documents(user_id, kind);

CREATE TABLE document_content_versions (
    id TEXT PRIMARY KEY,
    document_id TEXT NOT NULL REFERENCES user_documents(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL,
    version_number INTEGER NOT NULL,
    source TEXT NOT NULL,
    content_kind TEXT NOT NULL CHECK (content_kind IN ('resume_json', 'cover_text', 'original_file_ref')),
    content_json TEXT NOT NULL,
    profile_snapshot_json TEXT,
    profile_snapshot_hash TEXT,
    market TEXT,
    document_language TEXT,
    style_name TEXT,
    css_file_path TEXT,
    css_snapshot TEXT NOT NULL DEFAULT '',
    renderer_version TEXT NOT NULL DEFAULT '',
    reconstructible INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(document_id, version_number)
);
CREATE INDEX idx_document_content_versions_user ON document_content_versions(user_id);
CREATE INDEX idx_document_content_versions_doc ON document_content_versions(document_id);

CREATE TABLE document_original_files (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    document_id TEXT NOT NULL REFERENCES user_documents(id) ON DELETE CASCADE,
    content_version_id TEXT NOT NULL REFERENCES document_content_versions(id) ON DELETE CASCADE,
    filename TEXT NOT NULL,
    media_type TEXT NOT NULL DEFAULT 'application/octet-stream',
    storage_key TEXT NOT NULL,
    sha256 TEXT NOT NULL,
    byte_size INTEGER NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_document_original_files_user ON document_original_files(user_id);

CREATE TABLE document_render_artifacts (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    content_version_id TEXT NOT NULL REFERENCES document_content_versions(id) ON DELETE CASCADE,
    storage_key TEXT NOT NULL,
    sha256 TEXT NOT NULL,
    byte_size INTEGER NOT NULL,
    renderer_version TEXT NOT NULL,
    template_identity TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'ready' CHECK (state IN ('pending', 'ready', 'failed')),
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(user_id, sha256)
);
CREATE INDEX idx_document_render_artifacts_version ON document_render_artifacts(content_version_id);

CREATE TABLE document_version_artifact_refs (
    content_version_id TEXT PRIMARY KEY REFERENCES document_content_versions(id) ON DELETE CASCADE,
    artifact_id TEXT NOT NULL REFERENCES document_render_artifacts(id)
);

CREATE TABLE user_document_defaults (
    user_id TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('resume', 'cover_letter')),
    content_version_id TEXT NOT NULL REFERENCES document_content_versions(id),
    profile_snapshot_hash TEXT,
    market TEXT,
    selected_at TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (user_id, kind)
);

-- +goose Down
DROP TABLE IF EXISTS user_document_defaults;
DROP TABLE IF EXISTS document_version_artifact_refs;
DROP TABLE IF EXISTS document_render_artifacts;
DROP TABLE IF EXISTS document_original_files;
DROP TABLE IF EXISTS document_content_versions;
DROP TABLE IF EXISTS user_documents;
