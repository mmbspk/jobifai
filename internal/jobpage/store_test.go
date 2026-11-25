package jobpage

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	appdb "github.com/user/jobifai/internal/db"
)

func recipeStoreFixture(t *testing.T) (*SQLStore, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "reader.db")
	db, err := appdb.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec("INSERT INTO users(id,email,is_admin) VALUES ('reader-admin','reader-admin@example.test',1),('reader-user','reader-user@example.test',0),('other-user','other-user@example.test',0)")
	require.NoError(t, err)
	return NewSQLStore(db), path
}

func TestRecipeStoreRestartAndConcurrentLearningAreIdempotent(t *testing.T) {
	store, path := recipeStoreFixture(t)
	s, _, _, _, r := recipeFixture(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := store.SaveVerified(ctx, r); errs <- err }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	var count int
	require.NoError(t, store.DB.QueryRow("SELECT COUNT(*) FROM job_page_recipes").Scan(&count))
	require.Equal(t, 1, count)
	db, err := appdb.Open(path)
	require.NoError(t, err)
	defer db.Close()
	reopened := NewSQLStore(db)
	rows, err := reopened.Lookup(ctx, s.Scope, s.LayoutSignature)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	p, err := PlanFromRecipe(s, rows[0], DefaultLimits())
	require.NoError(t, err)
	require.Equal(t, []string{"n1"}, p.DescriptionRefs)
	_, err = reopened.SaveVerified(ctx, rows[0])
	require.Equal(t, FailureVerification, ErrorCode(err), "a loaded public recipe cannot claim new verification")
}

func TestRecipeStoreRejectsForgedAndMutatedPromotion(t *testing.T) {
	store, _ := recipeStoreFixture(t)
	_, _, _, _, r := recipeFixture(t)
	ctx := context.Background()
	raw, err := json.Marshal(r)
	require.NoError(t, err)
	var forged Recipe
	require.NoError(t, json.Unmarshal(raw, &forged))
	_, err = store.SaveVerified(ctx, forged)
	require.Equal(t, FailureVerification, ErrorCode(err))
	r.VerifiedAt = r.VerifiedAt.Add(time.Hour)
	_, err = store.SaveVerified(ctx, r)
	require.Equal(t, FailureVerification, ErrorCode(err))
}

func TestRecipeStoreInvalidationAndRetirementCannotBeResurrected(t *testing.T) {
	store, _ := recipeStoreFixture(t)
	s, p, live, v, r := recipeFixture(t)
	ctx := context.Background()
	_, err := store.SaveVerified(ctx, r)
	require.NoError(t, err)
	require.NoError(t, store.Invalidate(ctx, r.ID, "private details must never be persisted"))
	_, err = store.SaveVerified(ctx, r)
	require.ErrorIs(t, err, ErrInactiveRecipe)
	stored, err := store.GetRecipe(ctx, r.ID)
	require.NoError(t, err)
	require.Equal(t, "verification_failed", stored.StateReason)
	newer, err := BuildVerifiedRecipe(ctx, s, p, live, v, DefaultLimits())
	require.NoError(t, err)
	_, err = store.SaveVerified(ctx, newer)
	require.NoError(t, err)
	require.ErrorIs(t, store.RetireRecipe(ctx, "reader-user", r.ID), ErrForbidden)
	require.NoError(t, store.RetireRecipe(ctx, "reader-admin", r.ID))
	newer, err = BuildVerifiedRecipe(ctx, s, p, live, v, DefaultLimits())
	require.NoError(t, err)
	_, err = store.SaveVerified(ctx, newer)
	require.ErrorIs(t, err, ErrInactiveRecipe)
	rows, err := store.Lookup(ctx, s.Scope, s.LayoutSignature)
	require.NoError(t, err)
	require.Empty(t, rows)
	audit, err := store.ListAudit(ctx, 100)
	require.NoError(t, err)
	require.Len(t, audit, 1)
	require.Equal(t, "retire_recipe", audit[0].Action)
}

func TestRecipeStoreChecksChecksumAndPropagatesDatabaseFailures(t *testing.T) {
	store, _ := recipeStoreFixture(t)
	s, _, _, _, r := recipeFixture(t)
	ctx := context.Background()
	_, err := store.SaveVerified(ctx, r)
	require.NoError(t, err)
	_, err = store.DB.Exec("UPDATE job_page_recipes SET definition_json='{}' WHERE id=?", r.ID)
	require.NoError(t, err)
	_, err = store.Lookup(ctx, s.Scope, s.LayoutSignature)
	require.Equal(t, FailurePersistence, ErrorCode(err))
	require.NoError(t, store.DB.Close())
	_, err = store.SaveVerified(ctx, r)
	require.Equal(t, FailurePersistence, ErrorCode(err))
	_, err = store.GetConfig(ctx)
	require.Equal(t, FailurePersistence, ErrorCode(err))
}

func TestRecipeStoreWritesRollbackWhenAuditPersistenceFails(t *testing.T) {
	store, _ := recipeStoreFixture(t)
	_, _, _, _, r := recipeFixture(t)
	ctx := context.Background()
	_, err := store.SaveVerified(ctx, r)
	require.NoError(t, err)
	_, err = store.DB.Exec("DROP TABLE job_page_admin_audit")
	require.NoError(t, err)
	err = store.RetireRecipe(ctx, "reader-admin", r.ID)
	require.Equal(t, FailurePersistence, ErrorCode(err))
	stored, err := store.GetRecipe(ctx, r.ID)
	require.NoError(t, err)
	require.Equal(t, RecipeActive, stored.State)
}
