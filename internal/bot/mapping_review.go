package bot

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/user/jobifai/internal/domain"
)

type FormMappingReview struct {
	ID          string    `json:"id"`
	Field       SnapField `json:"field"`
	Platform    string    `json:"platform"`
	EmployerKey string    `json:"employer_key"`
	BindingKey  string    `json:"binding_key"`
	Provenance  string    `json:"provenance"`
}

// RecordProposals stores interpretations as pending, never applicant answers.
// Recording does not grant execution authority or activate a reusable mapping.
func (s *DBMappingStore) RecordProposals(ctx context.Context, snap StepSnapshot, interp StepInterpretation) error {
	if s == nil || s.DB == nil || s.ReadOnly || s.UserID == "" {
		return nil
	}
	for _, r := range interp.Fields {
		if r.Source != SourceLLM && r.Source != SourceReview {
			continue
		}
		f, ok := snap.FieldByRef(r.Ref)
		if !ok || f.Type == SnapTypeFile || f.Type == SnapTypeUnsupported {
			continue
		}
		f.BrowserField = nil
		f.ControlID = "" // live DOM identities are not reusable authority
		shape, err := json.Marshal(f)
		if err != nil {
			return err
		}
		_, err = s.DB.ExecContext(ctx, `
   INSERT INTO form_field_mappings
   (id,field_sig,field_label,field_type,field_group,source,action,intent_type,binding_key,provenance,
    resolved_value,user_id,employer_key,platform,schema_version,field_conditions_json)
   VALUES (?,?,?,?,?,'llm','fill','profile',?,'pending','',?,?,?,4,?)
   ON CONFLICT(field_sig,user_id,employer_key) DO UPDATE SET
    field_conditions_json=excluded.field_conditions_json,
    binding_key=CASE WHEN excluded.binding_key <> '' THEN excluded.binding_key ELSE form_field_mappings.binding_key END,
    updated_at=CURRENT_TIMESTAMP
   WHERE form_field_mappings.provenance NOT IN ('confirmed_by_reviewer','trusted_reference')`,
			uuid.NewString(), f.FieldSignature(snap.Platform), f.Label, string(f.Type), f.Group,
			r.BindingKey, s.UserID, snap.EmployerKey, snap.Platform, string(shape))
		if err != nil {
			return err
		}
	}
	return nil
}

func recordAdaptiveProposals(ctx context.Context, store MappingReadWriter, snap StepSnapshot, interp StepInterpretation) {
	if recorder, ok := store.(interface {
		RecordProposals(context.Context, StepSnapshot, StepInterpretation) error
	}); ok {
		_ = recorder.RecordProposals(ctx, snap, interp)
	}
}

func (s *DBMappingStore) InvalidateField(ctx context.Context, signature, employerKey string) error {
	if s == nil || s.DB == nil || s.ReadOnly || s.UserID == "" {
		return nil
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE form_field_mappings SET provenance='invalidated',updated_at=CURRENT_TIMESTAMP
	 WHERE field_sig=? AND user_id=? AND employer_key=?`, signature, s.UserID, employerKey)
	return err
}

func invalidateFailedMappings(ctx context.Context, store MappingReadWriter, snap StepSnapshot, interp StepInterpretation, result StepExecResult) {
	failed := StepInterpretation{}
	for _, ref := range result.FieldsFailed {
		failed.Fields = append(failed.Fields, FieldResolution{Ref: ref, Source: SourceReview, Action: ActionSkip, Pending: true, Reason: PendingInvalidOption})
	}
	recordAdaptiveProposals(ctx, store, snap, failed)
	invalidator, ok := store.(interface {
		InvalidateField(context.Context, string, string) error
	})
	if !ok {
		return
	}
	for _, ref := range result.FieldsFailed {
		for _, resolution := range interp.Fields {
			if resolution.Ref != ref || resolution.Source != SourceMapping {
				continue
			}
			if field, found := snap.FieldByRef(ref); found {
				_ = invalidator.InvalidateField(ctx, field.FieldSignature(snap.Platform), snap.EmployerKey)
			}
		}
	}
}

func ListFormMappingReviews(ctx context.Context, db *sql.DB, userID string) ([]FormMappingReview, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,field_conditions_json,platform,employer_key,binding_key,provenance
  FROM form_field_mappings WHERE user_id=? AND schema_version=4
  ORDER BY updated_at DESC,rowid DESC LIMIT 100`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []FormMappingReview{}
	for rows.Next() {
		var item FormMappingReview
		var shape string
		if err := rows.Scan(&item.ID, &shape, &item.Platform, &item.EmployerKey, &item.BindingKey, &item.Provenance); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(shape), &item.Field); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// ConfirmFormMapping is called only after an authenticated candidate explicitly
// confirms the question meaning and profile binding. A valid input alone never
// invokes this path. Promotion always remains user/employer scoped.
func ConfirmFormMapping(ctx context.Context, db *sql.DB, userID, id, bindingKey string, profile *domain.ResumeProfile) error {
	return confirmFormMapping(ctx, db, userID, id, bindingKey, profile)
}

func ConfirmFormMappingTx(ctx context.Context, tx *sql.Tx, userID, id, bindingKey string, profile *domain.ResumeProfile) error {
	return confirmFormMapping(ctx, tx, userID, id, bindingKey, profile)
}

type mappingQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func confirmFormMapping(ctx context.Context, db mappingQuery, userID, id, bindingKey string, profile *domain.ResumeProfile) error {
	if !knownProfileBinding(bindingKey) {
		return fmt.Errorf("unknown profile binding")
	}
	value, ok := resolveProfileBinding(bindingKey, profile)
	if !ok {
		return fmt.Errorf("profile answer is missing")
	}
	var shape string
	if err := db.QueryRowContext(ctx, `SELECT field_conditions_json FROM form_field_mappings WHERE id=? AND user_id=? AND schema_version=4`, id, userID).Scan(&shape); err != nil {
		return err
	}
	var field SnapField
	if err := json.Unmarshal([]byte(shape), &field); err != nil {
		return err
	}
	if !bindingValueCompatible(field, bindingKey, value) {
		return fmt.Errorf("profile answer does not match the current choices, type or limits")
	}
	result, err := db.ExecContext(ctx, `UPDATE form_field_mappings SET intent_type='profile',binding_key=?,resolved_value='',
  provenance='confirmed_by_reviewer',verified_by=?,verified_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP
  WHERE id=? AND user_id=? AND schema_version=4`, bindingKey, userID, id, userID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return domain.ErrNotFound
	}
	return nil
}
