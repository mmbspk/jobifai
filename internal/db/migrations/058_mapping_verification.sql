-- +goose Up
-- Browser acceptance validates mechanics, not the meaning of an answer.
-- Retire pre-verification mappings and clear saved applicant answers. Rows stay
-- available for diagnostic inspection but cannot be replayed by schema v4.
UPDATE form_field_mappings
SET provenance = 'invalidated', resolved_value = '', updated_at = CURRENT_TIMESTAMP;
ALTER TABLE form_field_mappings ADD COLUMN verified_by TEXT NOT NULL DEFAULT '';
ALTER TABLE form_field_mappings ADD COLUMN verified_at DATETIME;
ALTER TABLE form_field_mappings ADD COLUMN field_conditions_json TEXT NOT NULL DEFAULT '{}';

-- +goose Down
-- Retired answers cannot be reconstructed. Restore the prior schema shape.
ALTER TABLE form_field_mappings DROP COLUMN field_conditions_json;
ALTER TABLE form_field_mappings DROP COLUMN verified_at;
ALTER TABLE form_field_mappings DROP COLUMN verified_by;
