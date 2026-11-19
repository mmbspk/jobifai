package bot

// mapping.go — SQLite-backed form field mapping store for Story #137.
//
// DBMappingStore uses per-field signatures (SnapField.FieldSignature) as cache keys,
// enabling field-level reuse: adding or removing other fields on a page does not
// invalidate unrelated cached entries (story #138 blocker 3).
//
// The store records HOW to handle a field (source, action) AND the resolved value
// (resolved_value column, story #138 blocker 2). Warm hits fill the correct answer
// without calling the model.
//
// LLM-proposed resolutions are written as 'verified' once the form step is successfully
// executed — the form accepting the value is the trusted reference evidence.
// Profile/deterministic resolutions are verified immediately at write time.
//
// Shadow-mode observations are NEVER written here; only adaptive-mode runs may
// promote a resolution (ReadOnly=true blocks all writes).
//
// Schema key: (field_sig, user_id) where user_id='' means globally shared.

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

// MappingLookup is the hook for field-level verified mapping reuse.
// LookupField looks up a single field by its platform-scoped identity hash.
// Returns false on a cold miss or when the mapping is not yet verified.
type MappingLookup interface {
	LookupField(fieldSig string) (FieldResolution, bool)
}

// MappingVerificationState is the lifecycle state of a stored mapping entry.
type MappingVerificationState string

const (
	MappingVerified    MappingVerificationState = "verified"
	MappingInvalidated MappingVerificationState = "invalidated"
)

const mappingSchemaVersion = 2

// DBMappingStore is a SQLite-backed MappingLookup + writer.
// ReadOnly=true disables writes (use in shadow mode to block accidental promotion).
type DBMappingStore struct {
	DB       *sql.DB
	UserID   string // "" = global/anonymous scope
	ReadOnly bool   // when true, Write is a no-op (shadow mode safety)
}

// LookupField implements MappingLookup. Returns a FieldResolution if a verified
// mapping exists for fieldSig under this store's user scope.
// Falls back to the global scope (user_id='') if no user-scoped entry found.
// The returned Value is the stored resolved_value (the warm-hit answer).
func (s *DBMappingStore) LookupField(fieldSig string) (FieldResolution, bool) {
	if s == nil || s.DB == nil {
		return FieldResolution{}, false
	}

	var source, action, value string
	err := s.DB.QueryRow(`
		SELECT source, action, resolved_value FROM form_field_mappings
		WHERE field_sig = ? AND verification_state = 'verified'
		  AND (user_id = ? OR user_id = '')
		ORDER BY CASE WHEN user_id = '' THEN 1 ELSE 0 END
		LIMIT 1`,
		fieldSig, s.UserID,
	).Scan(&source, &action, &value)
	if err != nil {
		return FieldResolution{}, false
	}

	return FieldResolution{
		Source: SourceMapping,
		Action: AllowedAction(action),
		Value:  value,
	}, true
}

// Write persists verified, non-pending resolutions from an adaptive apply step.
// Resolutions with Pending=true or Source=SourceReview are silently skipped.
// ReadOnly stores ignore all writes (shadow mode).
//
// snap provides the platform (for field_sig computation) and field metadata
// (label, type) for each resolution.
//
// LLM-sourced resolutions are written as 'verified' — the form accepting the
// value during ExecuteStep is the trusted reference evidence.
// Profile and deterministic resolutions are also written as 'verified'.
func (s *DBMappingStore) Write(ctx context.Context, snap StepSnapshot, resolutions []FieldResolution) error {
	if s == nil || s.DB == nil || s.ReadOnly {
		return nil
	}

	now := time.Now().UTC()
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

		fieldSig := f.FieldSignature(snap.Platform)
		id := uuid.NewString()
		_, err := s.DB.ExecContext(ctx, `
			INSERT INTO form_field_mappings
				(id, field_sig, field_label, field_type, source, action, resolved_value,
				 user_id, platform, schema_version, verification_state, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'verified', ?, ?)
			ON CONFLICT(field_sig, user_id) DO UPDATE SET
				source             = excluded.source,
				action             = excluded.action,
				resolved_value     = excluded.resolved_value,
				field_label        = excluded.field_label,
				field_type         = excluded.field_type,
				verification_state = 'verified',
				updated_at         = excluded.updated_at`,
			id, fieldSig, f.Label, string(f.Type),
			string(r.Source), string(r.Action), r.Value,
			s.UserID, snap.Platform, mappingSchemaVersion, now, now,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

// Invalidate marks one mapping entry as invalidated so it is excluded from future lookups.
// fieldSig is the SnapField.FieldSignature(platform) value for the target field.
// A missing entry is a no-op (idempotent).
func (s *DBMappingStore) Invalidate(ctx context.Context, fieldSig string) error {
	if s == nil || s.DB == nil {
		return nil
	}
	_, err := s.DB.ExecContext(ctx, `
		UPDATE form_field_mappings SET verification_state = 'invalidated', updated_at = CURRENT_TIMESTAMP
		WHERE field_sig = ? AND user_id = ?`,
		fieldSig, s.UserID,
	)
	return err
}
