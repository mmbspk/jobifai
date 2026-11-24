-- +goose Up
-- Upgrade form_field_mappings to field-level cache keys (story #138 / epic #127).
--
-- Changes from migration 055:
--   1. field_sig (SnapField.FieldSignature) replaces (snapshot_sig, field_ref) as the
--      unique identity of a cached mapping.  This enables field-level reuse: adding or
--      removing other fields on a page no longer invalidates unrelated entries.
--   2. resolved_value stores the LLM-proposed (or profile-derived) value so warm hits
--      can fill the correct answer without calling the model again.
--   3. Verification states: 'verified' (produced by profile/deterministic rules or LLM
--      after a successful form step), 'pending_verify' (LLM-proposed but step not yet
--      executed), 'invalidated' (explicitly retired by the system or reviewer).
--
-- The old table is dropped and recreated; no production data exists yet.

DROP INDEX IF EXISTS idx_fmaps_sig_user;
DROP TABLE IF EXISTS form_field_mappings;

CREATE TABLE form_field_mappings (
    id              TEXT NOT NULL PRIMARY KEY,
    field_sig       TEXT NOT NULL,                   -- SnapField.FieldSignature(platform)
    field_label     TEXT NOT NULL,                   -- normalized label (debugging)
    field_type      TEXT NOT NULL,                   -- SnapFieldType string
    source          TEXT NOT NULL,                   -- profile|deterministic|llm
    action          TEXT NOT NULL,                   -- fill|upload|skip
    resolved_value  TEXT NOT NULL DEFAULT '',        -- value to fill on warm hit
    user_id         TEXT NOT NULL DEFAULT '',        -- '' = globally shared
    platform        TEXT NOT NULL DEFAULT '',
    schema_version  INTEGER NOT NULL DEFAULT 2,
    verification_state TEXT NOT NULL DEFAULT 'verified',  -- verified|pending_verify|invalidated
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(field_sig, user_id)
);

CREATE INDEX idx_fmaps_fieldsig_user ON form_field_mappings(field_sig, user_id, verification_state);

-- +goose Down
DROP INDEX IF EXISTS idx_fmaps_fieldsig_user;
DROP TABLE IF EXISTS form_field_mappings;

-- Restore 055 schema.
CREATE TABLE form_field_mappings (
    id              TEXT NOT NULL PRIMARY KEY,
    snapshot_sig    TEXT NOT NULL,
    field_ref       TEXT NOT NULL,
    field_label     TEXT NOT NULL,
    field_type      TEXT NOT NULL,
    source          TEXT NOT NULL,
    action          TEXT NOT NULL,
    user_id         TEXT NOT NULL DEFAULT '',
    platform        TEXT NOT NULL DEFAULT '',
    schema_version  INTEGER NOT NULL DEFAULT 1,
    verification_state TEXT NOT NULL DEFAULT 'verified',
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(snapshot_sig, field_ref, user_id)
);
CREATE INDEX idx_fmaps_sig_user ON form_field_mappings(snapshot_sig, user_id, verification_state);
