-- +goose Up
-- Form field mappings: verified structural bindings learned from adaptive apply runs.
-- Keyed by (snapshot_sig, field_ref, user_id) where user_id='' means globally shared.
-- A mapping stores how to handle a field (source, action) but NOT the candidate answer.
-- Answers are always resolved fresh from the current profile at replay time.
-- Shadow-mode observations are never written here; only adaptive-mode verified results.
CREATE TABLE IF NOT EXISTS form_field_mappings (
    id              TEXT NOT NULL PRIMARY KEY,
    snapshot_sig    TEXT NOT NULL,         -- StepSnapshot.SemanticSignature()
    field_ref       TEXT NOT NULL,         -- SnapField.Ref within that snapshot
    field_label     TEXT NOT NULL,         -- normalized label (for human debugging)
    field_type      TEXT NOT NULL,         -- SnapFieldType string
    source          TEXT NOT NULL,         -- deterministic|profile|llm
    action          TEXT NOT NULL,         -- fill|upload|skip
    user_id         TEXT NOT NULL DEFAULT '',  -- '' = generic/shared scope
    platform        TEXT NOT NULL DEFAULT '',
    schema_version  INTEGER NOT NULL DEFAULT 1,
    verification_state TEXT NOT NULL DEFAULT 'verified',  -- verified|invalidated
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(snapshot_sig, field_ref, user_id)
);
CREATE INDEX IF NOT EXISTS idx_fmaps_sig_user ON form_field_mappings(snapshot_sig, user_id, verification_state);

-- +goose Down
DROP INDEX IF EXISTS idx_fmaps_sig_user;
DROP TABLE IF EXISTS form_field_mappings;
