package bot_test

import (
	"context"
	"database/sql"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/bot"
	"github.com/user/jobifai/internal/browser"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/domain"
)

// setupManagerWithSink creates a Manager wired to a RunLogSink for lifecycle tests.
// Returns the manager, sink, underlying DB (for failure injection), and user ID.
func setupManagerWithSink(t *testing.T) (*bot.Manager, *bot.RunLogSink, *sql.DB, string) {
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
	return mgr, sink, db, userID
}

// TestManager_StandaloneAcquireRun_BlocksConcurrentStart verifies that a standalone
// operation (runSubmit or ApplyFromURL with no active search run) reserves entry.runID
// under the manager lock before calling Register. A concurrent Start() must see the
// slot as occupied and return "already running", not race to call Register and wipe
// the standalone operation's log history.
func TestManager_StandaloneAcquireRun_BlocksConcurrentStart(t *testing.T) {
	mgr, _, _, userID := setupManagerWithSink(t)

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
	mgr, sink, _, userID := setupManagerWithSink(t)

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

// TestManager_AcquireRun_FailedRegistration_LeavesCleanStateAndUnblocksSecondCaller
// verifies the failure path: when Register fails, acquireRun rolls back the reservation
// atomically under m.mu, leaving a clean slot so that a subsequent caller can acquire
// its own standalone registration successfully.
//
// A cancelled context is used to make Register fail deterministically (BeginTx
// returns immediately on an already-cancelled context).
func TestManager_AcquireRun_FailedRegistration_LeavesCleanStateAndUnblocksSecondCaller(t *testing.T) {
	mgr, sink, _, userID := setupManagerWithSink(t)

	// Use a pre-cancelled context to force Register to fail.
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	// First call fails: Register cannot begin a transaction on a cancelled context.
	_, _, err := mgr.AcquireRunForTest(cancelledCtx, userID, "run-fail")
	require.Error(t, err, "acquireRun must fail when Register returns an error")

	// Manager state must be clean: no dangling reservation from the failed call.
	status := mgr.Status(userID)
	assert.Equal(t, "", status.AutomationRunID,
		"failed registration must not leave a stale AutomationRunID in status")

	// Start() must not be blocked by a dangling runID.
	// (Will fail for other reasons — no config — but NOT "already running".)
	startErr := mgr.Start(context.Background(), userID, domain.Platform("test_automation"))
	if startErr != nil {
		assert.NotContains(t, startErr.Error(), "already running",
			"failed registration must release the run slot so Start is not blocked")
	}

	// A second acquireRun with a valid context must succeed standalone, proving the
	// slot was fully released — not partially held — after the first failure.
	runID2, cleanup2, err2 := mgr.AcquireRunForTest(context.Background(), userID, "run-after-failure")
	require.NoError(t, err2, "second acquireRun after failure must succeed")
	assert.Equal(t, "run-after-failure", runID2,
		"second acquire must register a fresh run, not inherit the failed one")
	defer cleanup2()

	// Log lines for the new run must persist (proves the new run is fully registered).
	_, werr := sink.Write(makeLine(userID, runID2, "lid-recovery"))
	require.NoError(t, werr)
	requireRowCount(t, sink, userID, 1)
}

// TestManager_AcquireRun_ConcurrentCalls_SecondWaitsForRegistration verifies that when
// two goroutines call acquireRun concurrently, the second cannot proceed before the first
// has completed registration.
//
// Mechanism: acquireRun holds m.mu throughout Register. A second goroutine blocks at
// m.mu.Lock() and can only see entry.runID after the first's Register call returns.
// This structural guarantee means seeing entry.runID != "" is always sufficient evidence
// that registration is complete and successful — no "registering-in-progress" state exists.
//
// Observable invariant: both callers return the same run ID (one owns it, one inherits),
// and exactly one Register call is made to the sink (verified by row count after writes).
func TestManager_AcquireRun_ConcurrentCalls_SecondWaitsForRegistration(t *testing.T) {
	mgr, sink, _, userID := setupManagerWithSink(t)

	ctx := context.Background()

	var (
		runID1, runID2         string
		cleanup1, cleanup2     func()
		err1, err2             error
		wg                     sync.WaitGroup
	)

	wg.Add(2)
	go func() {
		defer wg.Done()
		runID1, cleanup1, err1 = mgr.AcquireRunForTest(ctx, userID, "run-concurrent-1")
	}()
	go func() {
		defer wg.Done()
		runID2, cleanup2, err2 = mgr.AcquireRunForTest(ctx, userID, "run-concurrent-2")
	}()
	wg.Wait()

	require.NoError(t, err1)
	require.NoError(t, err2)

	// Both goroutines must use the same run ID: whichever goroutine acquired m.mu first
	// registered its proposedID; the second goroutine inherited it.
	assert.Equal(t, runID1, runID2,
		"both concurrent acquireRun calls must return the same run ID")

	// Exactly one Register call was made (verified by checking bot_current_run).
	// If two registers had fired, the second would have deleted all rows from the first.
	// Write two distinct lines — both must persist.
	_, werr := sink.Write(makeLine(userID, runID1, "lid-c1"))
	require.NoError(t, werr)
	_, werr = sink.Write(makeLine(userID, runID2, "lid-c2"))
	require.NoError(t, werr)
	requireRowCount(t, sink, userID, 2)

	if cleanup1 != nil {
		cleanup1()
	}
	if cleanup2 != nil {
		cleanup2()
	}
}

// requireRowCount asserts the number of persisted log rows via LatestRun.
func requireRowCount(t *testing.T, sink *bot.RunLogSink, userID string, want int) {
	t.Helper()
	got, err := sink.LatestRun(context.Background(), userID)
	require.NoError(t, err)
	assert.Len(t, got.Lines, want, "expected %d persisted log rows for %s", want, userID)
}
