-- +goose Up
-- Migration 057: schema version 3 for form_field_mappings.
--
-- Changes from schema version 2 (migration 056):
--   • intent_type   — 'profile' (resolved at runtime from profile binding) or 'llm' (opaque value).
--   • binding_key   — dotted profile path, e.g. 'application_defaults.notice_period'. Empty for 'llm' intent.
--   • provenance    — 'accepted_by_form' | 'confirmed_by_reviewer'. Lookup only returns trusted rows.
--   • employer_key  — company name / ATS tenant from the job posting. Empty = globally applicable.
--   • field_group   — section heading (fieldset/legend), included in the field signature.
--   • UNIQUE key    — (field_sig, user_id, employer_key). Employer-specific answers no longer collide.
--
-- All rows from schema version 2 are discarded (DROP + recreate), not migrated.
-- The old 'verification_state' column is replaced by 'provenance'.

CREATE TABLE IF NOT EXISTS form_field_mappings_v3 (
    id              TEXT    NOT NULL,
    field_sig       TEXT    NOT NULL,
    field_label     TEXT    NOT NULL DEFAULT '',
    field_type      TEXT    NOT NULL DEFAULT '',
    field_group     TEXT    NOT NULL DEFAULT '',
    source          TEXT    NOT NULL DEFAULT '',
    action          TEXT    NOT NULL DEFAULT '',
    intent_type     TEXT    NOT NULL DEFAULT 'llm',
    binding_key     TEXT    NOT NULL DEFAULT '',
    provenance      TEXT    NOT NULL DEFAULT 'accepted_by_form',
    resolved_value  TEXT    NOT NULL DEFAULT '',
    user_id         TEXT    NOT NULL DEFAULT '',
    employer_key    TEXT    NOT NULL DEFAULT '',
    platform        TEXT    NOT NULL DEFAULT '',
    schema_version  INTEGER NOT NULL DEFAULT 3,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE (field_sig, user_id, employer_key)
);

CREATE INDEX IF NOT EXISTS idx_fmaps_v3_lookup
    ON form_field_mappings_v3 (field_sig, user_id, employer_key, provenance);

-- Atomically replace the old table, invalidating all legacy rows.
DROP TABLE IF EXISTS form_field_mappings;
ALTER TABLE form_field_mappings_v3 RENAME TO form_field_mappings;

-- +goose Down
-- Recreate the schema-version-2 table so a rollback restores the prior shape.
-- Existing data in the v3 table is discarded on rollback just as it was on upgrade.

CREATE TABLE IF NOT EXISTS form_field_mappings_v2 (
    id                 TEXT NOT NULL,
    field_sig          TEXT NOT NULL,
    field_label        TEXT NOT NULL DEFAULT '',
    field_type         TEXT NOT NULL DEFAULT '',
    source             TEXT NOT NULL DEFAULT '',
    action             TEXT NOT NULL DEFAULT '',
    resolved_value     TEXT NOT NULL DEFAULT '',
    user_id            TEXT NOT NULL DEFAULT '',
    platform           TEXT NOT NULL DEFAULT '',
    schema_version     INTEGER NOT NULL DEFAULT 2,
    verification_state TEXT NOT NULL DEFAULT 'verified',
    created_at         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE (field_sig, user_id)
);

CREATE INDEX IF NOT EXISTS idx_fmaps_fieldsig_user
    ON form_field_mappings_v2 (field_sig, user_id, verification_state);

DROP TABLE IF EXISTS form_field_mappings;
ALTER TABLE form_field_mappings_v2 RENAME TO form_field_mappings;
