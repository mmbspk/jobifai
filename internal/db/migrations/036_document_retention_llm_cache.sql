-- +goose Up
CREATE TABLE document_retention_audit (
    id TEXT PRIMARY KEY,
    admin_user_id TEXT NOT NULL,
    previous_limit INTEGER NOT NULL,
    new_limit INTEGER NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_document_retention_audit_created ON document_retention_audit(created_at DESC);

CREATE TABLE llm_generation_cache (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    task TEXT NOT NULL,
    content_fingerprint TEXT NOT NULL,
    visual_identity_hash TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL CHECK (state IN ('pending', 'in_progress', 'completed', 'failed_uncertain')),
    response_text TEXT NOT NULL DEFAULT '',
    operation_id TEXT NOT NULL DEFAULT '',
    lease_owner TEXT NOT NULL DEFAULT '',
    lease_until TEXT,
    provider_uncertain INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(user_id, task, content_fingerprint)
);
CREATE INDEX idx_llm_generation_cache_state ON llm_generation_cache(state, lease_until);

-- +goose Down
DROP TABLE IF EXISTS llm_generation_cache;
DROP TABLE IF EXISTS document_retention_audit;
