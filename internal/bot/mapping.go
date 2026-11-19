package bot

// mapping.go — SQLite-backed form field mapping store for Story #137.
//
// DBMappingStore implements MappingLookup and writes verified resolutions
// from adaptive-mode apply runs. Shadow-mode observations are NEVER written
// here; only adaptive-mode runs may promote a resolution.
//
// Schema key: (snapshot_sig, field_ref, user_id).
// user_id="" means globally generic (shared across all users on the same instance).
//
// The store records HOW to handle a field (source, action) but NOT the
// candidate's answer. Answers are resolved fresh from the current profile at
// replay time by the deterministic or profile path; the mapping merely signals
// that the field is known and need not go to the LLM again.

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

// MappingVerificationState is the lifecycle state of a stored mapping entry.
type MappingVerificationState string

const (
	MappingVerified    MappingVerificationState = "verified"
	MappingInvalidated MappingVerificationState = "invalidated"
)

const mappingSchemaVersion = 1

// DBMappingStore is a SQLite-backed MappingLookup + writer.
// ReadOnly=true disables writes (use in shadow mode to block accidental promotion).
type DBMappingStore struct {
	DB       *sql.DB
	UserID   string // "" = global/anonymous scope
	ReadOnly bool   // when true, Write is a no-op (shadow mode safety)
}

// Lookup implements MappingLookup. Returns a FieldResolution if a verified
// mapping exists for (sig, fieldRef) under this store's user scope.
// Falls back to the global scope (user_id='') if no user-scoped entry found.
func (s *DBMappingStore) Lookup(sig, fieldRef string) (FieldResolution, bool) {
	if s == nil || s.DB == nil {
		return FieldResolution{}, false
	}

	var source, action string
	err := s.DB.QueryRow(`
		SELECT source, action FROM form_field_mappings
		WHERE snapshot_sig = ? AND field_ref = ? AND verification_state = 'verified'
		  AND (user_id = ? OR user_id = '')
		ORDER BY CASE WHEN user_id = '' THEN 1 ELSE 0 END
		LIMIT 1`,
		sig, fieldRef, s.UserID,
	).Scan(&source, &action)
	if err != nil {
		return FieldResolution{}, false
	}

	return FieldResolution{
		Ref:    fieldRef,
		Source: SourceMapping,
		Action: AllowedAction(action),
	}, true
}

// Write persists verified, non-pending resolutions from an adaptive apply step.
// Resolutions with Pending=true or Source=SourceReview are silently skipped.
// ReadOnly stores ignore all writes (shadow mode).
//
// snap is used to populate metadata (label, type) for each field.
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

		id := uuid.NewString()
		_, err := s.DB.ExecContext(ctx, `
			INSERT INTO form_field_mappings
				(id, snapshot_sig, field_ref, field_label, field_type, source, action,
				 user_id, platform, schema_version, verification_state, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'verified', ?, ?)
			ON CONFLICT(snapshot_sig, field_ref, user_id) DO UPDATE SET
				source             = excluded.source,
				action             = excluded.action,
				field_label        = excluded.field_label,
				field_type         = excluded.field_type,
				verification_state = 'verified',
				updated_at         = excluded.updated_at`,
			id, snap.SemanticSignature(), r.Ref, f.Label, string(f.Type),
			string(r.Source), string(r.Action),
			s.UserID, snap.Platform, mappingSchemaVersion, now, now,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

// Invalidate marks one mapping entry as invalidated so it is excluded from future lookups.
// A missing entry is a no-op (idempotent).
func (s *DBMappingStore) Invalidate(ctx context.Context, sig, fieldRef string) error {
	if s == nil || s.DB == nil {
		return nil
	}
	_, err := s.DB.ExecContext(ctx, `
		UPDATE form_field_mappings SET verification_state = 'invalidated', updated_at = CURRENT_TIMESTAMP
		WHERE snapshot_sig = ? AND field_ref = ? AND user_id = ?`,
		sig, fieldRef, s.UserID,
	)
	return err
}
