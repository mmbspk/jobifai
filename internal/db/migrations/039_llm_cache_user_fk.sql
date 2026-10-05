-- +goose Up
-- Recreate llm_generation_cache with FOREIGN KEY user_id → users(id) ON DELETE CASCADE
-- so that any attempt to acquire a new cache lease for a deleted user is rejected at
-- the database level.  The data copy excludes rows whose user_id no longer exists.
CREATE TABLE llm_generation_cache_new (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
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
INSERT INTO llm_generation_cache_new
    SELECT * FROM llm_generation_cache
    WHERE user_id IN (SELECT id FROM users);
DROP TABLE llm_generation_cache;
ALTER TABLE llm_generation_cache_new RENAME TO llm_generation_cache;
CREATE INDEX idx_llm_generation_cache_state ON llm_generation_cache(state, lease_until);

-- +goose Down
CREATE TABLE llm_generation_cache_old (
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
INSERT INTO llm_generation_cache_old SELECT * FROM llm_generation_cache;
DROP TABLE llm_generation_cache;
ALTER TABLE llm_generation_cache_old RENAME TO llm_generation_cache;
CREATE INDEX idx_llm_generation_cache_state ON llm_generation_cache(state, lease_until);
