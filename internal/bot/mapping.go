package bot

// mapping.go — SQLite-backed form field mapping store for Story #137.
//
// DBMappingStore uses per-field signatures (SnapField.FieldSignature) as cache keys,
// enabling field-level reuse: adding or removing other fields on a page does not
// invalidate unrelated cached entries (story #138 blocker 3).
//
// The store records HOW to handle a field (source, action, intent, binding) AND the
// resolved value (resolved_value column). Warm hits fill the correct answer without
// calling the model; profile-bound hits resolve from the candidate's current data.
//
// LLM-proposed resolutions are written as provenance='accepted_by_form' once the
// form step is successfully executed — the form accepting the value is the trusted
// reference evidence.
//
// Shadow-mode observations are NEVER written here; only adaptive-mode runs may
// promote a resolution (ReadOnly=true blocks all writes).
//
// Schema key: (field_sig, user_id, employer_key).
// user_id='' means globally shared; employer_key='' means employer-agnostic.

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
)

// MappingReadWriter combines lookup and write for the adaptive apply pipeline.
// DBMappingStore satisfies this interface; shadow-mode stores set ReadOnly=true
// so Write is a no-op.
type MappingReadWriter interface {
	MappingLookup
	Write(ctx context.Context, snap StepSnapshot, resolutions []FieldResolution) error
}

const mappingSchemaVersion = 3

// DBMappingStore is a SQLite-backed MappingLookup + writer (schema version 3).
// ReadOnly=true disables writes (use in shadow mode to block accidental promotion).
// EmployerKey scopes mappings to a specific company / ATS tenant; "" = any employer.
type DBMappingStore struct {
	DB          *sql.DB
	UserID      string // "" = global/anonymous scope
	EmployerKey string // "" = employer-agnostic
	ReadOnly    bool   // when true, Write is a no-op (shadow mode safety)
}

// LookupField implements MappingLookup. Returns a trusted FieldResolution if one
// exists for (fieldSig, employerKey) under this store's user scope.
// Falls back to the global scope (user_id='') and to employer-agnostic rows
// (employer_key='') when no exact match is found.
// Only provenance='accepted_by_form' or 'confirmed_by_reviewer' rows are returned.
// The IntentType and BindingKey fields are populated so the caller can resolve
// profile-bound entries from the current candidate data at runtime.
func (s *DBMappingStore) LookupField(fieldSig, employerKey string) (FieldResolution, bool) {
	if s == nil || s.DB == nil {
		return FieldResolution{}, false
	}

	var source, action, intentType, bindingKey, value string
	err := s.DB.QueryRow(`
		SELECT source, action, intent_type, binding_key, resolved_value
		FROM form_field_mappings
		WHERE field_sig = ?
		  AND provenance IN ('accepted_by_form', 'confirmed_by_reviewer')
		  AND (user_id = ? OR user_id = '')
		  AND (employer_key = ? OR employer_key = '')
		ORDER BY
		  CASE WHEN user_id      = '' THEN 1 ELSE 0 END,
		  CASE WHEN employer_key = '' THEN 1 ELSE 0 END
		LIMIT 1`,
		fieldSig, s.UserID, employerKey,
	).Scan(&source, &action, &intentType, &bindingKey, &value)
	if err != nil {
		return FieldResolution{}, false
	}

	return FieldResolution{
		Source:     SourceMapping,
		Action:     AllowedAction(action),
		Value:      value,
		IntentType: FieldIntentType(intentType),
		BindingKey: bindingKey,
		Provenance: ProvenanceAcceptedByForm,
	}, true
}

// Write persists trusted, non-pending resolutions from an adaptive apply step.
// Resolutions with Pending=true or Source=SourceReview are silently skipped.
// ReadOnly stores ignore all writes (shadow mode).
//
// snap provides the platform, employer key, and field metadata for each resolution.
//
// All written entries receive provenance='accepted_by_form' — the form accepting
// the value during ExecuteStep is the trusted reference evidence.
func (s *DBMappingStore) Write(ctx context.Context, snap StepSnapshot, resolutions []FieldResolution) error {
	if s == nil || s.DB == nil || s.ReadOnly {
		return nil
	}

	now := time.Now().UTC()
	employerKey := snap.EmployerKey
	for _, r := range resolutions {
		if r.Pending {
			continue // never write unresolved fields
		}
		if r.Source == SourceReview {
			continue // human-review items are not stable mappings
		}
		if r.Source == SourceMapping {
			continue // already in the store; no-op
		}

		f, ok := snap.FieldByRef(r.Ref)
		if !ok {
			continue
		}

		intentType := string(r.IntentType)
		if intentType == "" {
			intentType = string(FieldIntentLLM)
		}

		fieldSig := f.FieldSignature(snap.Platform)
		id := uuid.NewString()
		_, err := s.DB.ExecContext(ctx, `
			INSERT INTO form_field_mappings
				(id, field_sig, field_label, field_type, field_group, source, action,
				 intent_type, binding_key, provenance, resolved_value,
				 user_id, employer_key, platform, schema_version, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'accepted_by_form', ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(field_sig, user_id, employer_key) DO UPDATE SET
				source         = excluded.source,
				action         = excluded.action,
				intent_type    = excluded.intent_type,
				binding_key    = excluded.binding_key,
				provenance     = excluded.provenance,
				resolved_value = excluded.resolved_value,
				field_label    = excluded.field_label,
				field_type     = excluded.field_type,
				field_group    = excluded.field_group,
				updated_at     = excluded.updated_at`,
			id, fieldSig, f.Label, string(f.Type), f.Group,
			string(r.Source), string(r.Action),
			intentType, r.BindingKey, r.Value,
			s.UserID, employerKey, snap.Platform, mappingSchemaVersion, now, now,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

// Invalidate marks all matching mapping entries as provenance='invalidated' so
// they are excluded from future lookups. fieldSig is the SnapField.FieldSignature
// value. A missing entry is a no-op (idempotent).
func (s *DBMappingStore) Invalidate(ctx context.Context, fieldSig string) error {
	if s == nil || s.DB == nil {
		return nil
	}
	_, err := s.DB.ExecContext(ctx, `
		UPDATE form_field_mappings
		   SET provenance = 'invalidated', updated_at = CURRENT_TIMESTAMP
		 WHERE field_sig = ? AND user_id = ?`,
		fieldSig, s.UserID,
	)
	return err
}
