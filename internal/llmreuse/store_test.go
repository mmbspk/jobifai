package llmreuse_test

import (
	"context"
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
	return &llmreuse.Store{DB: sqldb}
}

func TestContentFingerprint_UserIsolation(t *testing.T) {
	t.Parallel()
	msgs := []llmreuse.MessagePart{{Role: "user", Content: "hello"}}
	a := llmreuse.ContentFingerprint("user-a", "tailoring", "claude", "m1", 8192, msgs)
	b := llmreuse.ContentFingerprint("user-b", "tailoring", "claude", "m1", 8192, msgs)
	require.NotEqual(t, a, b)
}

func TestVisualIdentityHash_SeparateFromContent(t *testing.T) {
	t.Parallel()
	v1 := llmreuse.VisualIdentityHash("classic", "au", "css-a")
	v2 := llmreuse.VisualIdentityHash("modern", "au", "css-b")
	require.NotEqual(t, v1, v2)
	msgs := []llmreuse.MessagePart{{Role: "user", Content: "same"}}
	fp := llmreuse.ContentFingerprint("u1", "tailoring", "claude", "m1", 8192, msgs)
	require.NotEmpty(t, fp)
}

func TestStore_BeginComplete_CacheHit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := openReuseDB(t)
	msgs := []llmreuse.MessagePart{{Role: "user", Content: "prompt"}}
	fp := llmreuse.ContentFingerprint("u1", "tailoring", "claude", "m1", 8192, msgs)
	br1, err := store.Begin(ctx, "u1", "tailoring", fp, "vis", "op-1")
	require.NoError(t, err)
	require.True(t, br1.LeaseAcquired)
	require.NoError(t, store.Complete(ctx, "u1", "tailoring", fp, "op-1", "generated body"))
	br2, err := store.Begin(ctx, "u1", "tailoring", fp, "vis-other", "op-2")
	require.NoError(t, err)
	require.True(t, br2.CacheHit)
	require.Equal(t, "generated body", br2.Response)
}

func TestStore_ConcurrentBegin_WaitsForInProgressLease(t *testing.T) {
	ctx := context.Background()
	store := openReuseDB(t)
	msgs := []llmreuse.MessagePart{{Role: "user", Content: "race"}}
	fp := llmreuse.ContentFingerprint("u1", "cover_letter", "claude", "m1", 8192, msgs)

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
	require.NoError(t, store.Complete(ctx, "u1", "cover_letter", fp, "op-race", "ok"))
	br2 := <-done
	require.True(t, br2.CacheHit)
	require.Equal(t, "ok", br2.Response)
}

func TestStore_MarkFailedUncertain_AllowsFreshProviderCall(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := openReuseDB(t)
	msgs := []llmreuse.MessagePart{{Role: "user", Content: "uncertain"}}
	fp := llmreuse.ContentFingerprint("u1", "tailoring", "claude", "m1", 8192, msgs)
	br, err := store.Begin(ctx, "u1", "tailoring", fp, "", "op-crash")
	require.NoError(t, err)
	require.True(t, br.LeaseAcquired)
	require.NoError(t, store.MarkFailedUncertain(ctx, "u1", "tailoring", fp, "op-crash"))
	br2, err := store.Begin(ctx, "u1", "tailoring", fp, "", "op-retry")
	require.NoError(t, err)
	require.True(t, br2.LeaseAcquired)
	require.False(t, br2.CacheHit)
}
