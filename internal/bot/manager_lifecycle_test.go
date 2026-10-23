package bot_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/bot"
	"github.com/user/jobifai/internal/browser"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/domain"
)

// setupManagerWithSink creates a Manager wired to a RunLogSink for lifecycle tests.
func setupManagerWithSink(t *testing.T) (*bot.Manager, *bot.RunLogSink, string) {
	t.Helper()
	db := openTestDB(t)
	const userID = "lifecycle-user"
	createUser(t, db, userID)

	sink := bot.NewRunLogSink(db)
	cfgStore := config.NewStore(db)
	secrets := config.NewSecretsStore(db, "test-lifecycle-key")
	sessions := browser.NewSessionStore(db, secrets)

	ctx := context.Background()
	mgr := bot.NewManager(ctx, db, cfgStore, secrets, sessions, nil, nil, nil, nil, "")
	mgr.SetRunLogSink(sink)
	return mgr, sink, userID
}

// TestManager_StandaloneAcquireRun_BlocksConcurrentStart verifies that a standalone
// operation (runSubmit or ApplyFromURL with no active search run) reserves entry.runID
// under the manager lock before calling Register. A concurrent Start() must see the
// slot as occupied and return "already running", not race to call Register and wipe
// the standalone operation's log history.
func TestManager_StandaloneAcquireRun_BlocksConcurrentStart(t *testing.T) {
	mgr, _, userID := setupManagerWithSink(t)

	ctx := context.Background()

	// Simulate a standalone operation: acquireRun reserves the slot.
	runID, cleanup, err := mgr.AcquireRunForTest(ctx, userID, "standalone-run-1")
	require.NoError(t, err)
	assert.Equal(t, "standalone-run-1", runID)

	// Start() must return "already running" while the slot is held.
	startErr := mgr.Start(ctx, userID, domain.Platform("test_automation"))
	require.Error(t, startErr)
	assert.Contains(t, startErr.Error(), "already running")

	// Release the slot.
	cleanup()

	// Status should now reflect no active run (AutomationRunID cleared).
	status := mgr.Status(userID)
	assert.Equal(t, "", status.AutomationRunID)
}

// TestManager_SharedRun_RefcountKeepsRegistrationAlive verifies that when a search run
// and an approval share the same run registration, the log sink stays registered until
// the last participating operation releases it. Releasing just the first ref must not
// Unregister the sink, so the second operation can continue writing.
func TestManager_SharedRun_RefcountKeepsRegistrationAlive(t *testing.T) {
	mgr, sink, userID := setupManagerWithSink(t)

	ctx := context.Background()

	// First operation (standalone, e.g. a direct apply): acquireRun creates the slot.
	runID, cleanupFirst, err := mgr.AcquireRunForTest(ctx, userID, "shared-run-1")
	require.NoError(t, err)
	require.Equal(t, "shared-run-1", runID)

	// Second operation (e.g. an approval inheriting the same run): acquireRun inherits.
	inherited, cleanupSecond, err := mgr.AcquireRunForTest(ctx, userID, "proposed-ignored")
	require.NoError(t, err)
	require.Equal(t, runID, inherited, "second acquire must inherit the active run")

	// Write one log line while both refs are held.
	_, werr := sink.Write(makeLine(userID, runID, "lid-1"))
	require.NoError(t, werr)
	requireRowCount(t, sink, userID, 1)

	// First ref released (simulates the direct apply finishing) — refcount drops from
	// 2 to 1; the sink must NOT be unregistered yet.
	cleanupFirst()

	// Second operation can still write (registration still active).
	_, werr = sink.Write(makeLine(userID, runID, "lid-2"))
	require.NoError(t, werr)
	requireRowCount(t, sink, userID, 2)

	// Second ref released (simulates the approval finishing) — refcount drops to 0;
	// sink is unregistered; further writes are silently dropped.
	cleanupSecond()

	_, werr = sink.Write(makeLine(userID, runID, "lid-3"))
	require.NoError(t, werr)
	requireRowCount(t, sink, userID, 2) // still 2, not 3
}

// requireRowCount asserts the number of persisted log rows via LatestRun.
func requireRowCount(t *testing.T, sink *bot.RunLogSink, userID string, want int) {
	t.Helper()
	got, err := sink.LatestRun(context.Background(), userID)
	require.NoError(t, err)
	assert.Len(t, got.Lines, want, "expected %d persisted log rows for %s", want, userID)
}
