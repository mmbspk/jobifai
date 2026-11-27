package bot

// mapping.go — SQLite-backed form field mapping store for Story #137.
//
// DBMappingStore uses per-field signatures (SnapField.FieldSignature) as cache keys,
// enabling field-level reuse: adding or removing other fields on a page does not
// invalidate unrelated cached entries (story #138 blocker 3).
//
// The store records how to interpret a field and which current profile fact it
// binds. Direct bindings never retain an applicant value; independently verified
// derived answers are cached only in the candidate/issuer scope with a profile
// digest and checked evidence. Unverified model proposals remain pending.
//
// Shadow-mode observations are NEVER written here; only adaptive-mode runs may
// promote a resolution (ReadOnly=true blocks all writes).
//
// Schema key: (field_sig, user_id, employer_key).
// user_id='' means globally shared; employer_key='' means employer-agnostic.

import (
	"context"
	"database/sql"
	"encoding/json"
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
// Automatically verified model rows require their private candidate/issuer scope
// and a versioned verification record. Legacy opaque answers cannot activate.
// The IntentType and BindingKey fields are populated so the caller can resolve
// profile-bound entries from the current candidate data at runtime.
func (s *DBMappingStore) LookupField(fieldSig, employerKey string) (FieldResolution, bool) {
	if s == nil || s.DB == nil {
		return FieldResolution{}, false
	}

	var action, intentType, bindingKey, provenance, value, conditions string
	err := s.DB.QueryRow(`
		SELECT action, intent_type, binding_key, provenance, resolved_value, field_conditions_json
		FROM form_field_mappings
		WHERE field_sig = ?
		  AND schema_version = 4
		  AND (
		    (provenance IN ('trusted_reference', 'confirmed_by_reviewer') AND intent_type IN ('profile', 'preferences') AND binding_key <> '')
		    OR (provenance = 'verified_by_model' AND user_id = ? AND user_id <> '' AND employer_key = ?
		        AND verified_by = 'grounded-form-answer-v1' AND verified_at IS NOT NULL
		        AND (intent_type = 'llm' OR (intent_type = 'profile' AND binding_key <> '')))
		  )
		  AND (user_id = ? OR user_id = '')
		  AND (employer_key = ? OR employer_key = '')
		ORDER BY
		  CASE WHEN user_id      = '' THEN 1 ELSE 0 END,
		  CASE WHEN employer_key = '' THEN 1 ELSE 0 END
		LIMIT 1`,
		fieldSig, s.UserID, employerKey, s.UserID, employerKey,
	).Scan(&action, &intentType, &bindingKey, &provenance, &value, &conditions)
	if err != nil {
		return FieldResolution{}, false
	}

	resolution := FieldResolution{
		Source:     SourceMapping,
		Action:     AllowedAction(action),
		IntentType: FieldIntentType(intentType),
		BindingKey: bindingKey,
		Provenance: VerificationProvenance(provenance),
	}
	if resolution.Provenance == ProvenanceVerifiedByModel {
		var shape groundedMappingConditions
		if json.Unmarshal([]byte(conditions), &shape) != nil || shape.Grounded == nil ||
			shape.Grounded.Version != groundedAnswerVersion || shape.Grounded.FieldSig != fieldSig || action != string(ActionFill) {
			return FieldResolution{}, false
		}
		resolution.Value, resolution.cached = value, shape.Grounded
	}
	return resolution, true
}

// Write persists trusted, non-pending resolutions from an adaptive apply step.
// Resolutions with Pending=true or Source=SourceReview are silently skipped.
// ReadOnly stores ignore all writes (shadow mode).
//
// snap provides the platform, employer key, and field metadata for each resolution.
//
// Independently grounded model answers activate automatically after successful
// local filling. Unverified proposals remain pending; a successful transition
// alone never approves question meaning.
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
		verified := r.Provenance == ProvenanceVerifiedByModel && r.grounded != nil
		preference := r.IntentType == FieldIntentPreferences && r.Source == SourcePreferences && knownWorkPreferenceBinding(r.BindingKey)
		usableIntent := (r.IntentType == FieldIntentProfile && knownProfileBinding(r.BindingKey)) || preference || (verified && r.IntentType == FieldIntentLLM && r.BindingKey == "")
		if r.Action != ActionFill || !usableIntent {
			continue // applicant answers and navigation actions are never reusable mappings
		}

		f, ok := snap.FieldByRef(r.Ref)
		if !ok {
			continue
		}
		if preference && genericWorkArrangementBinding(f) != r.BindingKey {
			continue
		}
		if verified && (r.grounded.profileDigest == "" || r.grounded.platform != snap.Platform || r.grounded.employer != snap.EmployerKey ||
			r.grounded.fieldSig != f.FieldSignature(snap.Platform) || r.grounded.answerDigest != groundedAnswerDigest(r.IntentType, r.BindingKey, r.Value) || manualDeclaration(f)) {
			continue
		}
		if r.Provenance == ProvenanceVerifiedByModel && !verified {
			continue // a claimed provenance without an internal proof is insufficient
		}

		intentType := string(r.IntentType)
		if intentType == "" {
			intentType = string(FieldIntentLLM)
		}
		provenance := ProvenancePending
		if r.Source == SourceProfile || r.Source == SourceDeterministic || r.Source == SourcePreferences {
			provenance = ProvenanceTrustedReference
		}
		value, verifiedBy := "", ""
		var verifiedAt any
		f.BrowserField, f.ControlID = nil, ""
		shape := groundedMappingConditions{SnapField: f}
		if verified {
			if s.UserID == "" {
				continue // never publish candidate answers or evidence to global scope
			}
			provenance, verifiedBy, verifiedAt = ProvenanceVerifiedByModel, groundedAnswerVerifier, now
			shape.Grounded = &groundedCacheRecord{Version: groundedAnswerVersion, ProfileDigest: r.grounded.profileDigest, FieldSig: r.grounded.fieldSig, AnswerDigest: r.grounded.answerDigest, Evidence: append([]ProfileEvidence(nil), r.grounded.evidence...)}
			if r.IntentType == FieldIntentLLM {
				value = r.Value
			} else if directBindingIndependentOfValue(f, r.BindingKey) {
				shape.Grounded.ProfileDigest, shape.Grounded.Evidence = "", nil
			}
		}
		conditions, err := json.Marshal(shape)
		if err != nil {
			return err
		}

		fieldSig := f.FieldSignature(snap.Platform)
		id := uuid.NewString()
		_, err = s.DB.ExecContext(ctx, `
			INSERT INTO form_field_mappings
				(id, field_sig, field_label, field_type, field_group, source, action,
				 intent_type, binding_key, provenance, resolved_value,
				 user_id, employer_key, platform, schema_version, created_at, updated_at,
				 field_conditions_json, verified_by, verified_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(field_sig, user_id, employer_key) DO UPDATE SET
				source         = excluded.source,
				action         = excluded.action,
				intent_type    = excluded.intent_type,
				binding_key    = excluded.binding_key,
				provenance     = CASE WHEN form_field_mappings.provenance = 'confirmed_by_reviewer' AND form_field_mappings.binding_key = excluded.binding_key THEN form_field_mappings.provenance ELSE excluded.provenance END,
				resolved_value = excluded.resolved_value,
				field_label    = excluded.field_label,
				field_type     = excluded.field_type,
				field_group    = excluded.field_group,
				schema_version = excluded.schema_version,
				field_conditions_json = excluded.field_conditions_json,
				verified_by    = excluded.verified_by,
				verified_at    = excluded.verified_at,
				updated_at     = excluded.updated_at`,
			id, fieldSig, f.Label, string(f.Type), f.Group,
			string(r.Source), string(r.Action),
			intentType, r.BindingKey, string(provenance), value,
			s.UserID, employerKey, snap.Platform, mappingSchemaVersion, now, now,
			string(conditions), verifiedBy, verifiedAt,
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
