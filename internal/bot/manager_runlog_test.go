package bot_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/bot"
	"github.com/user/jobifai/internal/browser"
	"github.com/user/jobifai/internal/config"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/resume"
	"github.com/user/jobifai/internal/testutil/mockllm"

	_ "modernc.org/sqlite" // driver needed for raw sql.Open
)

// newManagerWithRunLogSink creates a fully wired manager with a real RunLogSink.
// The sinkDB parameter may be any *sql.DB; pass one without migrations to
// induce Register failures.
func newManagerWithRunLogSink(
	t *testing.T,
	sinkDB *sql.DB,
) (*bot.Manager, *sql.DB, *config.Store, *config.SecretsStore, *browser.SessionStore) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "mgr.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	cfgStore := config.NewStore(db)
	secrets := config.NewSecretsStore(db, "test-key-rl")
	sessions := browser.NewSessionStore(db, secrets)

	srv := mockllm.ClaudeServer(t, `{"score": 5, "reasoning": "test"}`)
	t.Cleanup(srv.Close)
	scoreClient := mockllm.NewClaudeClient(t, srv)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	scorer := resume.NewScorer(scoreClient)
	renderer := resume.NewPDFRenderer("resume_style")
	mgr := bot.NewManager(ctx, db, cfgStore, secrets, sessions, nil, scorer, nil, renderer, "")

	sink := bot.NewRunLogSink(sinkDB)
	mgr.SetRunLogSink(sink)

	return mgr, db, cfgStore, secrets, sessions
}

// openNoMigrationDB opens a bare SQLite file with no schema.
// Calls to bot_current_run / bot_run_logs will fail with "no such table",
// simulating a Register() failure without touching the manager's own DB.
func openNoMigrationDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sink-fail.db")
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestManager_AdmitsOnlyOneRunPerUser verifies that a second Start() call for
// the same user while a run is active returns "already running".
func TestManager_AdmitsOnlyOneRunPerUser(t *testing.T) {
	scoreBody := `{"score": 9, "reasoning": "ok"}`
	srv := mockllm.ClaudeServer(t, scoreBody)
	t.Cleanup(srv.Close)

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	cfgStore := config.NewStore(db)
	secrets := config.NewSecretsStore(db, "test-key")
	sessions := browser.NewSessionStore(db, secrets)
	const userID = "admit-user"
	seedAutomationUser(t, userID, cfgStore, secrets, sessions, srv.URL)

	client := mockllm.NewClaudeClient(t, srv)
	scorer := resume.NewScorer(client)
	renderer := resume.NewPDFRenderer("resume_style")

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	mgr := bot.NewManager(ctx, db, cfgStore, secrets, sessions, nil, scorer, nil, renderer, "")
	require.NoError(t, mgr.Start(context.Background(), userID, bot.PlatformTestAutomation))

	// Second Start() for the same user while the first is still running/pending.
	err = mgr.Start(context.Background(), userID, bot.PlatformTestAutomation)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already running")
}

// TestManager_RegisterFailure_ClearsStatusAndRunID verifies that when
// RunLogSink.Register() fails, Start() returns an error and the bot entry's
// AutomationRunID is cleared rather than left dangling.
func TestManager_RegisterFailure_ClearsStatusAndRunID(t *testing.T) {
	// Sink uses a DB with no migrations → Register() fails with "no such table".
	sinkDB := openNoMigrationDB(t)
	mgr, _, cfgStore, secrets, sessions := newManagerWithRunLogSink(t, sinkDB)

	scoreBody := `{"score": 9, "reasoning": "ok"}`
	srv := mockllm.ClaudeServer(t, scoreBody)
	t.Cleanup(srv.Close)

	const userID = "fail-reg-user"
	seedAutomationUser(t, userID, cfgStore, secrets, sessions, srv.URL)

	err := mgr.Start(context.Background(), userID, bot.PlatformTestAutomation)
	require.Error(t, err, "Start() must fail when Register() fails")

	status := mgr.Status(userID)
	assert.Empty(t, status.AutomationRunID,
		"AutomationRunID must be cleared after Register() failure")
	assert.NotEqual(t, "running", string(status.State),
		"state must not be running after failed start")
}

// TestManager_RunLogSink_UnregisteredAfterStop verifies that Unregister is called
// when a run ends, so subsequent writes for that run are rejected by the sink.
func TestManager_RunLogSink_UnregisteredAfterStop(t *testing.T) {
	scoreBody := `{"score": 9, "reasoning": "ok"}`
	srv := mockllm.ClaudeServer(t, scoreBody)
	t.Cleanup(srv.Close)

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	cfgStore := config.NewStore(db)
	secrets := config.NewSecretsStore(db, "test-key")
	sessions := browser.NewSessionStore(db, secrets)
	const userID = "stop-unregister-user"
	seedAutomationUser(t, userID, cfgStore, secrets, sessions, srv.URL)

	client := mockllm.NewClaudeClient(t, srv)
	scorer := resume.NewScorer(client)
	renderer := resume.NewPDFRenderer("resume_style")

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	mgr := bot.NewManager(ctx, db, cfgStore, secrets, sessions, nil, scorer, nil, renderer, "")
	createUser(t, db, userID)
	sink := bot.NewRunLogSink(db)
	mgr.SetRunLogSink(sink)

	require.NoError(t, mgr.Start(context.Background(), userID, bot.PlatformTestAutomation))

	// Bot should be running — AutomationRunID set.
	statusBefore := mgr.Status(userID)
	runID := statusBefore.AutomationRunID
	require.NotEmpty(t, runID)

	// Stop the bot and wait for the run to complete and Unregister to fire.
	mgr.Stop(userID)
	require.Eventually(t, func() bool {
		s := mgr.Status(userID)
		return s.AutomationRunID == "" && s.State != "running"
	}, 5*time.Second, 20*time.Millisecond)

	// Verify LatestRun still returns the persisted run_id (sink keeps DB row).
	lr, lrErr := sink.LatestRun(context.Background(), userID)
	require.NoError(t, lrErr)
	assert.Equal(t, runID, lr.RunID,
		"LatestRun should still return the completed run_id after Unregister")
}
