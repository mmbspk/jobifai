package bot_test

// Black-box lifecycle tests for ApplyFromURL early exits.
// These verify the attempt-persistence contract for paths that fail before any
// browser is opened: unknown URL, setup failure, work guard rejection, and the
// DB-based already-applied fast path. No browser or LLM server required.

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/bot"
	"github.com/user/jobifai/internal/browser"
	"github.com/user/jobifai/internal/config"
	appdb "github.com/user/jobifai/internal/db"
)

// TestApplyFromURL_UnknownPlatform_NoAttemptRecorded verifies that a non-platform
// URL is rejected before any attempt row is written.
func TestApplyFromURL_UnknownPlatform_NoAttemptRecorded(t *testing.T) {
	db, mgr := newApplyTestManager(t)
	_, err := mgr.ApplyFromURL(context.Background(), "user-1", "https://example.com/not-a-job", "", false)
	require.Error(t, err)

	var n int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM apply_attempts WHERE user_id = ?`, "user-1").Scan(&n))
	assert.Equal(t, 0, n, "no attempt should be recorded for an unrecognised URL")
}

// TestApplyFromURL_SetupFailure_RecordsFailedAttempt verifies that when setupBot
// fails (no resume profile in DB), the attempt is persisted as failed/setup.
func TestApplyFromURL_SetupFailure_RecordsFailedAttempt(t *testing.T) {
	db, mgr := newApplyTestManager(t)
	_, err := mgr.ApplyFromURL(context.Background(), "user-setup-fail",
		"https://www.linkedin.com/jobs/view/123456789", "", false)
	require.Error(t, err)

	var outcome, stage string
	scanErr := db.QueryRow(
		`SELECT outcome, stage FROM apply_attempts WHERE user_id = ? ORDER BY started_at DESC LIMIT 1`,
		"user-setup-fail",
	).Scan(&outcome, &stage)
	require.NoError(t, scanErr, "a failed attempt must have been persisted")
	assert.Equal(t, "failed", outcome)
	assert.Equal(t, "setup", stage)
}

// TestApplyFromURL_WorkGuardFailure_RecordsFailedAttempt verifies that when the
// work guard rejects the call, the attempt is recorded as failed/work_guard.
func TestApplyFromURL_WorkGuardFailure_RecordsFailedAttempt(t *testing.T) {
	db, mgr := newApplyTestManager(t)
	mgr.SetSubmitWorkGuard(&alwaysFailWorkGuard{})

	_, err := mgr.ApplyFromURL(context.Background(), "user-wg-fail",
		"https://www.seek.com.au/job/123456789", "", false)
	require.Error(t, err)

	var outcome, stage string
	scanErr := db.QueryRow(
		`SELECT outcome, stage FROM apply_attempts WHERE user_id = ? ORDER BY started_at DESC LIMIT 1`,
		"user-wg-fail",
	).Scan(&outcome, &stage)
	require.NoError(t, scanErr, "a failed attempt must have been persisted")
	assert.Equal(t, "failed", outcome)
	assert.Equal(t, "work_guard", stage)
}

// TestApplyFromURL_AlreadyInDB_RecordsAlreadyApplied verifies the DB fast-path:
// if the URL is in jobs_applied, the call returns ErrAlreadyApplied and the
// attempt is recorded as already_applied (not a failure).
func TestApplyFromURL_AlreadyInDB_RecordsAlreadyApplied(t *testing.T) {
	db, mgr := newApplyTestManager(t)

	const userID = "user-already-applied"
	const jobURL = "https://www.linkedin.com/jobs/view/999888777"

	// Seed the jobs_applied table to trigger the fast-path.
	_, err := db.Exec(
		`INSERT INTO jobs_applied(id,user_id,platform,company,role,location,link,suitability_score,applied_at)
		 VALUES(?,?,?,?,?,?,?,?,?)`,
		"job-999", userID, "linkedin", "Contoso", "Nurse", "Sydney", jobURL, 80,
		time.Now().UTC().Format(time.RFC3339),
	)
	require.NoError(t, err)

	_, applyErr := mgr.ApplyFromURL(context.Background(), userID, jobURL, "", false)
	require.ErrorIs(t, applyErr, bot.ErrAlreadyApplied)

	var outcome, stage string
	scanErr := db.QueryRow(
		`SELECT outcome, stage FROM apply_attempts WHERE user_id = ? ORDER BY started_at DESC LIMIT 1`,
		userID,
	).Scan(&outcome, &stage)
	require.NoError(t, scanErr, "an already_applied attempt must have been persisted")
	assert.Equal(t, "already_applied", outcome)
	assert.Equal(t, "already_applied", stage)
}

// ── helpers ────────────────────────────────────────────────────────────────────

func newApplyTestManager(t *testing.T) (*sql.DB, *bot.Manager) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "manager_attempt_test.db")
	sqlDB, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	cfgStore := config.NewStore(sqlDB)
	secrets := config.NewSecretsStore(sqlDB, "test-key")
	sessions := browser.NewSessionStore(sqlDB, secrets)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	mgr := bot.NewManager(ctx, sqlDB, cfgStore, secrets, sessions, nil, nil, nil, nil, "")
	return sqlDB, mgr
}

// alwaysFailWorkGuard is a test double whose BeginWork always returns an error.
type alwaysFailWorkGuard struct{}

func (g *alwaysFailWorkGuard) BeginWork(_ context.Context, _ string) error {
	return errors.New("work guard: rejected")
}
func (g *alwaysFailWorkGuard) EndWork(_ string) {}
