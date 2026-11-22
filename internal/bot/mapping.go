package bot

// mapping.go — SQLite-backed form field mapping store for Story #137.
//
// DBMappingStore uses per-field signatures (SnapField.FieldSignature) as cache keys,
// enabling field-level reuse: adding or removing other fields on a page does not
// invalidate unrelated cached entries (story #138 blocker 3).
//
// The store records how to interpret a field and which current profile fact it
// binds. Applicant values are never retained in resolved_value.
//
// LLM proposals remain pending after successful execution. Independent candidate
// review is required before they can authorize reuse.
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

const mappingSchemaVersion = 4

// DBMappingStore is a SQLite-backed MappingLookup + writer (schema version 4).
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
// Falls back to the global scope (user_id=”) and to employer-agnostic rows
// (employer_key=”) when no exact match is found.
// Only trusted_reference or confirmed_by_reviewer rows are returned.
// The IntentType and BindingKey fields are populated so the caller can resolve
// profile-bound entries from the current candidate data at runtime.
func (s *DBMappingStore) LookupField(fieldSig, employerKey string) (FieldResolution, bool) {
	if s == nil || s.DB == nil {
		return FieldResolution{}, false
	}

	var action, intentType, bindingKey, provenance string
	err := s.DB.QueryRow(`
		SELECT action, intent_type, binding_key, provenance
		FROM form_field_mappings
		WHERE field_sig = ?
		  AND schema_version = 4
		  AND provenance IN ('trusted_reference', 'confirmed_by_reviewer')
		  AND intent_type = 'profile' AND binding_key <> ''
		  AND (user_id = ? OR user_id = '')
		  AND (employer_key = ? OR employer_key = '')
		ORDER BY
		  CASE WHEN user_id      = '' THEN 1 ELSE 0 END,
		  CASE WHEN employer_key = '' THEN 1 ELSE 0 END
		LIMIT 1`,
		fieldSig, s.UserID, employerKey,
	).Scan(&action, &intentType, &bindingKey, &provenance)
	if err != nil {
		return FieldResolution{}, false
	}

	return FieldResolution{
		Source:     SourceMapping,
		Action:     AllowedAction(action),
		IntentType: FieldIntentType(intentType),
		BindingKey: bindingKey,
		Provenance: VerificationProvenance(provenance),
	}, true
}

// Write persists trusted, non-pending resolutions from an adaptive apply step.
// Resolutions with Pending=true or Source=SourceReview are silently skipped.
// ReadOnly stores ignore all writes (shadow mode).
//
// snap provides the platform, employer key, and field metadata for each resolution.
//
// Model entries remain pending; deterministic profile handlers are trusted
// references. A successful transition never approves a model's interpretation.
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
		if r.Action != ActionFill || r.IntentType != FieldIntentProfile || !knownProfileBinding(r.BindingKey) {
			continue // applicant answers and navigation actions are never reusable mappings
		}

		f, ok := snap.FieldByRef(r.Ref)
		if !ok {
			continue
		}

		intentType := string(r.IntentType)
		if intentType == "" {
			intentType = string(FieldIntentLLM)
		}
		provenance := ProvenancePending
		if r.Source == SourceProfile || r.Source == SourceDeterministic {
			provenance = ProvenanceTrustedReference
		}

		fieldSig := f.FieldSignature(snap.Platform)
		id := uuid.NewString()
		_, err := s.DB.ExecContext(ctx, `
			INSERT INTO form_field_mappings
				(id, field_sig, field_label, field_type, field_group, source, action,
				 intent_type, binding_key, provenance, resolved_value,
				 user_id, employer_key, platform, schema_version, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?, ?, ?, ?, ?, ?)
			ON CONFLICT(field_sig, user_id, employer_key) DO UPDATE SET
				source         = excluded.source,
				action         = excluded.action,
				intent_type    = excluded.intent_type,
				binding_key    = excluded.binding_key,
				provenance     = CASE WHEN form_field_mappings.provenance = 'confirmed_by_reviewer' AND form_field_mappings.binding_key = excluded.binding_key THEN form_field_mappings.provenance ELSE excluded.provenance END,
				resolved_value = '',
				field_label    = excluded.field_label,
				field_type     = excluded.field_type,
				field_group    = excluded.field_group,
				schema_version = excluded.schema_version,
				updated_at     = excluded.updated_at`,
			id, fieldSig, f.Label, string(f.Type), f.Group,
			string(r.Source), string(r.Action),
			intentType, r.BindingKey, string(provenance),
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
