package llmreuse_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/llmreuse"
)

func openReuseDB(t *testing.T) *llmreuse.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	sqldb, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })
	// Migration 039 adds FK user_id → users on llm_generation_cache.
	// Seed the synthetic user so all tests can insert cache rows freely.
	_, err = sqldb.Exec(`INSERT INTO users (id, email, password_hash, is_admin, created_at)
		VALUES ('u1', 'u1@test.local', 'hash', 0, datetime('now'))`)
	require.NoError(t, err, "seed test user")
	return &llmreuse.Store{DB: sqldb}
}

func fp(t *testing.T, user, task, content string) string {
	t.Helper()
	msgs := []llmreuse.MessagePart{{Role: "user", Content: content}}
	return llmreuse.ContentFingerprint(user, task, "claude", "m1", "", 8192, msgs)
}

func TestContentFingerprint_UserIsolation(t *testing.T) {
	t.Parallel()
	msgs := []llmreuse.MessagePart{{Role: "user", Content: "hello"}}
	a := llmreuse.ContentFingerprint("user-a", "tailoring", "claude", "m1", "", 8192, msgs)
	b := llmreuse.ContentFingerprint("user-b", "tailoring", "claude", "m1", "", 8192, msgs)
	require.NotEqual(t, a, b)
}

func TestContentFingerprint_EffortIncluded(t *testing.T) {
	t.Parallel()
	msgs := []llmreuse.MessagePart{{Role: "user", Content: "hello"}}
	low := llmreuse.ContentFingerprint("u1", "t", "claude", "m1", "low", 8192, msgs)
	high := llmreuse.ContentFingerprint("u1", "t", "claude", "m1", "high", 8192, msgs)
	require.NotEqual(t, low, high)
}

func TestVisualIdentityHash_SeparateFromContent(t *testing.T) {
	t.Parallel()
	v1 := llmreuse.VisualIdentityHash("classic", "au", "css-a")
	v2 := llmreuse.VisualIdentityHash("modern", "au", "css-b")
	require.NotEqual(t, v1, v2)
	msgs := []llmreuse.MessagePart{{Role: "user", Content: "same"}}
	fp := llmreuse.ContentFingerprint("u1", "tailoring", "claude", "m1", "", 8192, msgs)
	require.NotEmpty(t, fp)
}

func TestStore_BeginComplete_CacheHit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := openReuseDB(t)
	fp := fp(t, "u1", "tailoring", "prompt")
	br1, err := store.Begin(ctx, "u1", "tailoring", fp, "vis", "op-1")
	require.NoError(t, err)
	require.True(t, br1.LeaseAcquired)
	require.Equal(t, "op-1", br1.OperationID)
	require.NotEmpty(t, br1.LeaseOwner)
	require.NoError(t, store.Complete(ctx, "u1", "tailoring", fp, br1.LeaseOwner, "generated body"))
	br2, err := store.Begin(ctx, "u1", "tailoring", fp, "vis-other", "op-2")
	require.NoError(t, err)
	require.True(t, br2.CacheHit)
	require.Equal(t, "generated body", br2.Response)
	require.Equal(t, "op-1", br2.OperationID)
}

func TestStore_ReclaimPreservesStoredOperationID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := openReuseDB(t)
	fp := fp(t, "u1", "tailoring", "reclaim")
	br1, err := store.Begin(ctx, "u1", "tailoring", fp, "", "logical-op-aaa")
	require.NoError(t, err)
	require.NoError(t, store.MarkFailedUncertain(ctx, "u1", "tailoring", fp, br1.LeaseOwner))
	br2, err := store.Begin(ctx, "u1", "tailoring", fp, "", "logical-op-bbb")
	require.NoError(t, err)
	require.True(t, br2.LeaseAcquired)
	require.Equal(t, "logical-op-aaa", br2.OperationID)
	require.NotEqual(t, br1.LeaseOwner, br2.LeaseOwner)
}

func TestStore_ConcurrentBegin_WaitsForInProgressLease(t *testing.T) {
	ctx := context.Background()
	store := openReuseDB(t)
	fp := fp(t, "u1", "cover_letter", "race")

	br1, err := store.Begin(ctx, "u1", "cover_letter", fp, "", "op-race")
	require.NoError(t, err)
	require.True(t, br1.LeaseAcquired)

	done := make(chan llmreuse.BeginResult, 1)
	go func() {
		br2, err := store.Begin(ctx, "u1", "cover_letter", fp, "", "op-wait")
		require.NoError(t, err)
		done <- br2
	}()
	time.Sleep(50 * time.Millisecond)
	require.NoError(t, store.Complete(ctx, "u1", "cover_letter", fp, br1.LeaseOwner, "ok"))
	br2 := <-done
	require.True(t, br2.CacheHit)
	require.Equal(t, "ok", br2.Response)
}

func TestStore_MarkFailedUncertain_AllowsFreshProviderCall(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := openReuseDB(t)
	fp := fp(t, "u1", "tailoring", "uncertain")
	br, err := store.Begin(ctx, "u1", "tailoring", fp, "", "op-crash")
	require.NoError(t, err)
	require.True(t, br.LeaseAcquired)
	require.NoError(t, store.MarkFailedUncertain(ctx, "u1", "tailoring", fp, br.LeaseOwner))
	br2, err := store.Begin(ctx, "u1", "tailoring", fp, "", "op-retry")
	require.NoError(t, err)
	require.True(t, br2.LeaseAcquired)
	require.False(t, br2.CacheHit)
}

func TestStore_StaleLeaseCompleteDoesNotPublishCache(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := openReuseDB(t)
	fp := fp(t, "u1", "tailoring", "stale")
	br1, err := store.Begin(ctx, "u1", "tailoring", fp, "", "op-stale")
	require.NoError(t, err)
	_, err = store.DB.ExecContext(ctx, `
		UPDATE llm_generation_cache SET lease_until = datetime('now', '-1 minute') WHERE user_id = ? AND content_fingerprint = ?`,
		"u1", fp)
	require.NoError(t, err)
	_, err = store.DB.ExecContext(ctx, `
		UPDATE llm_generation_cache SET state = ?, provider_uncertain = 1
		WHERE state = ? AND lease_until IS NOT NULL AND lease_until < datetime('now')`,
		llmreuse.StateFailedUncertain, llmreuse.StateInProgress)
	require.NoError(t, err)
	br2, err := store.Begin(ctx, "u1", "tailoring", fp, "", "op-new")
	require.NoError(t, err)
	require.True(t, br2.LeaseAcquired)
	require.ErrorIs(t, store.Complete(ctx, "u1", "tailoring", fp, br1.LeaseOwner, "stale-worker"), llmreuse.ErrLeaseLost)
	require.ErrorIs(t, store.MarkFailedUncertain(ctx, "u1", "tailoring", fp, br1.LeaseOwner), llmreuse.ErrLeaseLost)
	require.ErrorIs(t, store.RenewLease(ctx, "u1", "tailoring", fp, br1.LeaseOwner), llmreuse.ErrLeaseLost)
	var state string
	require.NoError(t, store.DB.QueryRowContext(ctx, `
		SELECT state FROM llm_generation_cache WHERE user_id = ? AND content_fingerprint = ?`, "u1", fp).Scan(&state))
	require.Equal(t, llmreuse.StateInProgress, state)
	require.NoError(t, store.Complete(ctx, "u1", "tailoring", fp, br2.LeaseOwner, "fresh-worker"))
	br4, err := store.Begin(ctx, "u1", "tailoring", fp, "", "op-final")
	require.NoError(t, err)
	require.True(t, br4.CacheHit)
	require.Equal(t, "fresh-worker", br4.Response)
}

func TestStore_RenewLease(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := openReuseDB(t)
	fp := fp(t, "u1", "tailoring", "renew")
	br, err := store.Begin(ctx, "u1", "tailoring", fp, "", "op-renew")
	require.NoError(t, err)
	require.NoError(t, store.RenewLease(ctx, "u1", "tailoring", fp, br.LeaseOwner))
	var until sql.NullString
	require.NoError(t, store.DB.QueryRowContext(ctx, `
		SELECT lease_until FROM llm_generation_cache WHERE user_id = ? AND content_fingerprint = ?`,
		"u1", fp).Scan(&until))
	require.True(t, until.Valid)
}
