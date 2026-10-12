package bot

// White-box lifecycle tests for attempt persistence.
// These tests exercise beginAttempt and finishAttempt against a real SQLite DB
// without any browser dependency.

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appdb "github.com/user/jobifai/internal/db"
)

func openAttemptTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "attempt_test.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newTestAC(userID, platform string) *AttemptContext {
	return newAttemptContext(userID, platform, "job-1", "https://example.com/job/1", "Analyst", "Acme Corp", "test")
}

// TestBeginAttempt_WritesInProgressRow verifies that beginAttempt inserts a row
// with outcome='in_progress' and a NULL finished_at.
func TestBeginAttempt_WritesInProgressRow(t *testing.T) {
	db := openAttemptTestDB(t)
	ac := newTestAC("u1", "linkedin")

	beginAttempt(db, ac)

	var outcome string
	var finishedAt sql.NullString
	err := db.QueryRow(
		`SELECT outcome, finished_at FROM apply_attempts WHERE id = ?`, ac.AttemptID,
	).Scan(&outcome, &finishedAt)
	require.NoError(t, err)
	assert.Equal(t, "in_progress", outcome)
	assert.False(t, finishedAt.Valid, "finished_at must be NULL after beginAttempt")
}

// TestFinishAttempt_UpdatesOutcomeAndFields verifies the UPDATE path when the row exists.
func TestFinishAttempt_UpdatesOutcomeAndFields(t *testing.T) {
	db := openAttemptTestDB(t)
	ac := newTestAC("u1", "linkedin")
	beginAttempt(db, ac)

	// Simulate enrichment after browser work.
	ac.JobID = "job-enriched"
	ac.Role = "Senior Analyst"
	ac.Company = "BetterCorp"

	finishAttempt(db, ac, OutcomeFailed, "browser", "browser_error", "rod: timeout")

	var outcome, stage, reasonCode, reasonText, jobID, role, company string
	var finishedAt sql.NullString
	err := db.QueryRow(
		`SELECT outcome, stage, reason_code, reason_text, job_id, role, company, finished_at
		 FROM apply_attempts WHERE id = ?`, ac.AttemptID,
	).Scan(&outcome, &stage, &reasonCode, &reasonText, &jobID, &role, &company, &finishedAt)
	require.NoError(t, err)
	assert.Equal(t, "failed", outcome)
	assert.Equal(t, "browser", stage)
	assert.Equal(t, "browser_error", reasonCode)
	assert.Equal(t, "rod: timeout", reasonText)
	assert.Equal(t, "job-enriched", jobID)
	assert.Equal(t, "Senior Analyst", role)
	assert.Equal(t, "BetterCorp", company)
	assert.True(t, finishedAt.Valid, "finished_at must be set after finishAttempt")
}

// TestFinishAttempt_FallbackInsert_WhenBeginAttemptSkipped verifies that
// finishAttempt writes a direct INSERT when no in-progress row exists.
func TestFinishAttempt_FallbackInsert_WhenBeginAttemptSkipped(t *testing.T) {
	db := openAttemptTestDB(t)
	ac := newTestAC("u1", "seek")
	// Deliberately skip beginAttempt — simulates DB unavailability at start.

	finishAttempt(db, ac, OutcomeSkipped, "score", "score_below_threshold", "score 3 < 7")

	var outcome, stage, reasonCode string
	var finishedAt sql.NullString
	err := db.QueryRow(
		`SELECT outcome, stage, reason_code, finished_at FROM apply_attempts WHERE id = ?`, ac.AttemptID,
	).Scan(&outcome, &stage, &reasonCode, &finishedAt)
	require.NoError(t, err, "fallback INSERT must write a row even when beginAttempt was never called")
	assert.Equal(t, "skipped", outcome)
	assert.Equal(t, "score", stage)
	assert.Equal(t, "score_below_threshold", reasonCode)
	assert.True(t, finishedAt.Valid, "fallback record must have finished_at set")
}

// TestBeginAttempt_NilDB_IsNoop verifies that a nil DB does not panic.
func TestBeginAttempt_NilDB_IsNoop(t *testing.T) {
	ac := newTestAC("u1", "linkedin")
	assert.NotPanics(t, func() { beginAttempt(nil, ac) })
}

// TestFinishAttempt_NilDB_IsNoop verifies that a nil DB does not panic.
func TestFinishAttempt_NilDB_IsNoop(t *testing.T) {
	ac := newTestAC("u1", "linkedin")
	assert.NotPanics(t, func() { finishAttempt(nil, ac, OutcomeFailed, "s", "c", "t") })
}

// TestBeginAttempt_ORIGNORE_IdempotentOnDuplicateID verifies that calling
// beginAttempt twice with the same attempt ID does not overwrite a finished outcome.
func TestBeginAttempt_ORIGNORE_IdempotentOnDuplicateID(t *testing.T) {
	db := openAttemptTestDB(t)
	ac := newTestAC("u1", "linkedin")

	beginAttempt(db, ac)
	finishAttempt(db, ac, OutcomeSubmittedConfirmed, "submitted", "", "")
	// Calling beginAttempt again with the same ID must be a no-op (INSERT OR IGNORE).
	beginAttempt(db, ac)

	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM apply_attempts WHERE id = ?`, ac.AttemptID).Scan(&count))
	assert.Equal(t, 1, count, "duplicate beginAttempt must not insert a second row")

	var outcome string
	require.NoError(t, db.QueryRow(`SELECT outcome FROM apply_attempts WHERE id = ?`, ac.AttemptID).Scan(&outcome))
	assert.Equal(t, "submitted_confirmed", outcome, "duplicate beginAttempt must not overwrite the finished outcome")
}

// TestAttemptLifecycle_CrashDetection verifies the crash-detection contract:
// a row left with outcome='in_progress' and NULL finished_at after a restart
// represents an interrupted attempt.
func TestAttemptLifecycle_CrashDetection(t *testing.T) {
	db := openAttemptTestDB(t)
	ac := newTestAC("u1", "seek")

	beginAttempt(db, ac)

	var n int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM apply_attempts
		 WHERE user_id = ? AND outcome = 'in_progress' AND finished_at IS NULL`,
		"u1",
	).Scan(&n)
	require.NoError(t, err)
	assert.Equal(t, 1, n, "crashed attempt must leave an in_progress row with NULL finished_at")
}

// TestAttemptOutcome_ElapsedMS verifies that elapsed_ms is set to a non-negative
// value after finishAttempt.
func TestAttemptOutcome_ElapsedMS(t *testing.T) {
	db := openAttemptTestDB(t)
	ac := newTestAC("u1", "linkedin")
	ac.StartedAt = time.Now().Add(-100 * time.Millisecond)

	beginAttempt(db, ac)
	finishAttempt(db, ac, OutcomeSubmittedConfirmed, "submitted", "", "")

	var elapsedMS int64
	require.NoError(t, db.QueryRow(`SELECT elapsed_ms FROM apply_attempts WHERE id = ?`, ac.AttemptID).Scan(&elapsedMS))
	assert.GreaterOrEqual(t, elapsedMS, int64(100), "elapsed_ms must reflect actual elapsed time")
}
