package jobpage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

var ErrConflict = errors.New("job page configuration revision changed")
var ErrForbidden = errors.New("job page admin permission required")
var ErrInactiveRecipe = errors.New("job page recipe has been retired or invalidated")

type SQLStore struct{ DB *sql.DB }

func NewSQLStore(db *sql.DB) *SQLStore { return &SQLStore{DB: db} }

func persistence(err error) error {
	if err == nil {
		return nil
	}
	return &ReadError{Code: FailurePersistence, Cause: err}
}

const recipeColumns = "id,platform,page_type,origin,snapshot_version,layout_signature,definition_json,definition_hash,verified_at_ns,state,state_reason,updated_at_ns"

type rowScanner interface{ Scan(...any) error }

func scanRecipe(row rowScanner) (Recipe, error) {
	var r Recipe
	var raw, hash string
	var verified, updated int64
	if err := row.Scan(&r.ID, &r.Scope.Platform, &r.Scope.PageType, &r.Scope.Origin, &r.Scope.SnapshotVersion, &r.LayoutSignature, &raw, &hash, &verified, &r.State, &r.StateReason, &updated); err != nil {
		return r, err
	}
	if len(raw) > 32*1024 || fmt.Sprintf("%x", sha256.Sum256([]byte(raw))) != hash {
		return r, errors.New("invalid stored recipe checksum")
	}
	var definition Recipe
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&definition); err != nil {
		return r, errors.New("invalid stored recipe definition")
	}
	if dec.Decode(new(any)) != io.EOF {
		return r, errors.New("invalid stored recipe trailing data")
	}
	if definition.ID != r.ID || definition.Scope != r.Scope || definition.LayoutSignature != r.LayoutSignature {
		return r, errors.New("invalid stored recipe scope")
	}
	r.Version, r.DescriptionSelectors, r.VerificationKind = definition.Version, definition.DescriptionSelectors, definition.VerificationKind
	r.VerifiedAt, r.UpdatedAt = time.Unix(0, verified).UTC(), time.Unix(0, updated).UTC()
	if err := validateRecipe(r); err != nil {
		return r, err
	}
	return r, nil
}

func (s *SQLStore) Lookup(ctx context.Context, scope Scope, signature string) ([]Recipe, error) {
	if !signaturePattern.MatchString(signature) {
		return nil, &ReadError{Code: FailurePlan}
	}
	rows, err := s.DB.QueryContext(ctx, "SELECT "+recipeColumns+" FROM job_page_recipes WHERE platform=? AND page_type=? AND origin=? AND snapshot_version=? AND layout_signature=? AND state='active' ORDER BY verified_at_ns DESC,id LIMIT 20", scope.Platform, scope.PageType, scope.Origin, scope.SnapshotVersion, signature)
	if err != nil {
		return nil, persistence(err)
	}
	defer func() { _ = rows.Close() }()
	result := []Recipe{}
	for rows.Next() {
		r, err := scanRecipe(rows)
		if err != nil {
			return nil, persistence(err)
		}
		result = append(result, r)
	}
	return result, persistence(rows.Err())
}

func (s *SQLStore) GetRecipe(ctx context.Context, id string) (Recipe, error) {
	r, err := scanRecipe(s.DB.QueryRowContext(ctx, "SELECT "+recipeColumns+" FROM job_page_recipes WHERE id=?", id))
	return r, persistence(err)
}

func (s *SQLStore) ListRecipes(ctx context.Context, state RecipeState, limit int) ([]Recipe, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	if state != "" && state != RecipeActive && state != RecipeInvalidated && state != RecipeRetired {
		return nil, &ReadError{Code: FailurePlan}
	}
	rows, err := s.DB.QueryContext(ctx, "SELECT "+recipeColumns+" FROM job_page_recipes WHERE (?='' OR state=?) ORDER BY updated_at_ns DESC,id LIMIT ?", state, state, limit)
	if err != nil {
		return nil, persistence(err)
	}
	defer func() { _ = rows.Close() }()
	result := []Recipe{}
	for rows.Next() {
		r, err := scanRecipe(rows)
		if err != nil {
			return nil, persistence(err)
		}
		result = append(result, r)
	}
	return result, persistence(rows.Err())
}

func (s *SQLStore) SaveVerified(ctx context.Context, r Recipe) (Recipe, error) {
	if err := ctx.Err(); err != nil {
		return Recipe{}, err
	}
	if r.seal == ([32]byte{}) || r.seal != recipeSeal(r) || r.State != RecipeActive {
		return Recipe{}, &ReadError{Code: FailureVerification}
	}
	if err := validateRecipe(r); err != nil {
		return Recipe{}, err
	}
	definition := r
	definition.UpdatedAt = time.Time{}
	definition.StateReason = ""
	raw, err := json.Marshal(definition)
	if err != nil {
		return Recipe{}, persistence(err)
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(raw))
	now := time.Now().UTC().UnixNano()
	_, err = s.DB.ExecContext(ctx, "INSERT INTO job_page_recipes ("+recipeColumns+") VALUES (?,?,?,?,?,?,?,?,?,'active','',?) ON CONFLICT(id) DO UPDATE SET definition_json=excluded.definition_json,definition_hash=excluded.definition_hash,verified_at_ns=excluded.verified_at_ns,state='active',state_reason='',updated_at_ns=excluded.updated_at_ns WHERE job_page_recipes.state!='retired' AND excluded.verified_at_ns>job_page_recipes.verified_at_ns AND (job_page_recipes.state='active' OR excluded.verified_at_ns>job_page_recipes.updated_at_ns)", r.ID, r.Scope.Platform, r.Scope.PageType, r.Scope.Origin, r.Scope.SnapshotVersion, r.LayoutSignature, string(raw), hash, r.VerifiedAt.UnixNano(), now)
	if err != nil {
		return Recipe{}, persistence(err)
	}
	stored, err := s.GetRecipe(ctx, r.ID)
	if err != nil {
		return Recipe{}, err
	}
	if stored.State != RecipeActive {
		return stored, ErrInactiveRecipe
	}
	return stored, nil
}

func safeStateReason(reason string) string {
	switch reason {
	case "layout_drift", "identity_drift", "ambiguous", "stale_snapshot", "incomplete", "operator_retired", "verification_failed":
		return reason
	default:
		return "verification_failed"
	}
}

func (s *SQLStore) Invalidate(ctx context.Context, id, reason string) error {
	_, err := s.DB.ExecContext(ctx, "UPDATE job_page_recipes SET state='invalidated',state_reason=?,updated_at_ns=? WHERE id=? AND state='active'", safeStateReason(reason), time.Now().UTC().UnixNano(), id)
	return persistence(err)
}

func requireAdmin(ctx context.Context, tx *sql.Tx, actor string) error {
	var admin bool
	if err := tx.QueryRowContext(ctx, "SELECT is_admin FROM users WHERE id=?", actor).Scan(&admin); err != nil || !admin {
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return persistence(err)
		}
		return ErrForbidden
	}
	return nil
}

func (s *SQLStore) RetireRecipe(ctx context.Context, actor, id string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return persistence(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := requireAdmin(ctx, tx, actor); err != nil {
		return err
	}
	now := time.Now().UTC().UnixNano()
	res, err := tx.ExecContext(ctx, "UPDATE job_page_recipes SET state='retired',state_reason='operator_retired',updated_at_ns=? WHERE id=? AND state!='retired'", now, id)
	if err != nil {
		return persistence(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return persistence(err)
	}
	if n > 0 {
		if _, err = tx.ExecContext(ctx, "INSERT INTO job_page_admin_audit(actor_user_id,action,subject_id,created_at_ns) VALUES (?,'retire_recipe',?,?)", actor, id, now); err != nil {
			return persistence(err)
		}
	}
	return persistence(tx.Commit())
}
