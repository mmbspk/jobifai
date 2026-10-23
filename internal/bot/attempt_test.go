package bot

// White-box lifecycle tests for attempt persistence.
// These tests exercise beginAttempt and finishAttempt against a real SQLite DB
// without any browser dependency.

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
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

// openPartialSchemaDB opens a SQLite DB that has ONLY the columns that
// finishAttempt's UPDATE needs (so the UPDATE succeeds with 0 rows) but is
// missing the columns that the fallback INSERT needs (so the INSERT fails).
// This lets tests verify the second "fallback INSERT also failed" warning path.
func openPartialSchemaDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "partial_schema.db")
	// Use the same sqlite driver (registered by appdb import) but without migrations.
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	// Include all columns the UPDATE's SET clause uses; omit user_id, platform,
	// entry_point, and started_at which the INSERT requires — so INSERT fails.
	_, err = db.Exec(`
		CREATE TABLE apply_attempts (
			id          TEXT PRIMARY KEY,
			job_id      TEXT,
			url         TEXT,
			role        TEXT,
			company     TEXT,
			outcome     TEXT,
			stage       TEXT,
			reason_code TEXT,
			reason_text TEXT,
			finished_at TEXT,
			elapsed_ms  INTEGER
		)
	`)
	require.NoError(t, err)
	return db
}

// captureLogs replaces the global zerolog logger with one writing to a buffer
// for the duration of the test. Returns the buffer.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	orig := log.Logger
	log.Logger = zerolog.New(buf)
	t.Cleanup(func() { log.Logger = orig })
	return buf
}

func newTestAC(userID, platform string) *AttemptContext {
	return newAttemptContext(userID, platform, "job-1", "https://example.com/job/1", "Analyst", "Acme Corp", "test", "")
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

// ── classifyApplyOutcome tests ─────────────────────────────────────────────

// TestClassifyApplyOutcome_DirectErrors verifies the three direct classifications.
func TestClassifyApplyOutcome_DirectErrors(t *testing.T) {
	assert.Equal(t, OutcomeCancelled, classifyApplyOutcome(context.Canceled))
	assert.Equal(t, OutcomeTimedOut, classifyApplyOutcome(context.DeadlineExceeded))
	assert.Equal(t, OutcomeFailed, classifyApplyOutcome(errors.New("something broke")))
}

// TestClassifyApplyOutcome_WrappedErrors verifies that errors.Is unwrapping works
// — the common production case is context.Canceled wrapped several layers deep
// (e.g. rod CDP error wrapping context.Canceled).
func TestClassifyApplyOutcome_WrappedErrors(t *testing.T) {
	wrappedCancel := fmt.Errorf("browser closed: %w", context.Canceled)
	wrappedTimeout := fmt.Errorf("navigate: %w", fmt.Errorf("dial tcp: %w", context.DeadlineExceeded))
	assert.Equal(t, OutcomeCancelled, classifyApplyOutcome(wrappedCancel))
	assert.Equal(t, OutcomeTimedOut, classifyApplyOutcome(wrappedTimeout))
}

// TestFinishAttempt_CancelledAndTimedOut verifies that the new outcome constants
// are persisted correctly to the DB.
func TestFinishAttempt_CancelledAndTimedOut(t *testing.T) {
	db := openAttemptTestDB(t)

	acCancel := newTestAC("u1", "linkedin")
	beginAttempt(db, acCancel)
	finishAttempt(db, acCancel, OutcomeCancelled, "apply", "apply_error", context.Canceled.Error())

	var outcome string
	require.NoError(t, db.QueryRow(`SELECT outcome FROM apply_attempts WHERE id = ?`, acCancel.AttemptID).Scan(&outcome))
	assert.Equal(t, "cancelled", outcome)

	acTimeout := newTestAC("u1", "seek")
	beginAttempt(db, acTimeout)
	finishAttempt(db, acTimeout, OutcomeTimedOut, "navigate", "navigate_failed", context.DeadlineExceeded.Error())

	require.NoError(t, db.QueryRow(`SELECT outcome FROM apply_attempts WHERE id = ?`, acTimeout.AttemptID).Scan(&outcome))
	assert.Equal(t, "timed_out", outcome)
}

// TestFinishAttempt_DiagnosticWarning_WhenBeginSkipped verifies that finishAttempt
// emits the "in-progress row missing" diagnostic warning when no beginAttempt row
// exists (UPDATE finds 0 rows). The fallback INSERT succeeds and the outcome is
// persisted.
func TestFinishAttempt_DiagnosticWarning_WhenBeginSkipped(t *testing.T) {
	db := openAttemptTestDB(t)
	ac := newTestAC("u1", "linkedin")
	buf := captureLogs(t)

	finishAttempt(db, ac, OutcomeFailed, "apply", "apply_error", "timeout")

	assert.True(t, strings.Contains(buf.String(), "in-progress row missing"),
		"diagnostic warning must be emitted when UPDATE finds 0 rows; log output: %s", buf.String())

	var outcome string
	require.NoError(t, db.QueryRow(`SELECT outcome FROM apply_attempts WHERE id = ?`, ac.AttemptID).Scan(&outcome))
	assert.Equal(t, "failed", outcome, "fallback INSERT must still persist the outcome")
}

// TestFinishAttempt_FallbackInsertFails_BothWarningsLogged verifies the path where
// UPDATE succeeds with 0 rows (no beginAttempt row, partial schema) and the fallback
// INSERT also fails (partial schema is missing required columns). Both the
// "in-progress row missing" and "fallback INSERT also failed" warnings must be logged.
func TestFinishAttempt_FallbackInsertFails_BothWarningsLogged(t *testing.T) {
	// Use a DB whose schema satisfies the UPDATE's SET columns but is missing the
	// columns that the fallback INSERT requires (user_id, platform, entry_point,
	// started_at), so the INSERT returns an error.
	db := openPartialSchemaDB(t)
	ac := newTestAC("u1", "seek")
	buf := captureLogs(t)

	finishAttempt(db, ac, OutcomeFailed, "apply", "apply_error", "ats error")

	out := buf.String()
	assert.True(t, strings.Contains(out, "in-progress row missing"),
		"must warn that the in-progress row is missing; log: %s", out)
	assert.True(t, strings.Contains(out, "fallback INSERT also failed"),
		"must warn when the fallback INSERT also fails; log: %s", out)
}

// ── queue outcome routing invariant ───────────────────────────────────────

// TestQueueApplyOutcome_RoutingTable tests the queueApplyOutcome production helper
// directly. This ensures the routing logic inside processApprovedQueue cannot break
// silently — any change to the routing must also break this test.
func TestQueueApplyOutcome_RoutingTable(t *testing.T) {
	// An error from easyApply/seekApply that triggers skipReasonBelongsInTopMatches:
	// must start with "easy apply:" or "seek apply:" and contain a blocked-marker keyword.
	blockedErr := errors.New("easy apply: blocked (external ATS — not automatable)")

	cases := []struct {
		name       string
		err        error
		outcome    AttemptOutcome
		stage      string
		reasonCode string
	}{
		{"seek submit unconfirmed", errSeekSubmitUnconfirmed, OutcomeSubmissionUnknown, "confirmation", "seek_submit_unconfirmed"},
		{"context cancelled", context.Canceled, OutcomeCancelled, "apply", "apply_error"},
		{"deadline exceeded", context.DeadlineExceeded, OutcomeTimedOut, "apply", "apply_error"},
		{"wrapped cancel", fmt.Errorf("browser: %w", context.Canceled), OutcomeCancelled, "apply", "apply_error"},
		{"blocked ATS", blockedErr, OutcomeBlocked, "apply", "not_automatable"},
		{"plain error", errors.New("unexpected DOM state"), OutcomeFailed, "apply", "apply_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			outcome, stage, reasonCode := queueApplyOutcome(tc.err)
			assert.Equal(t, tc.outcome, outcome)
			assert.Equal(t, tc.stage, stage)
			assert.Equal(t, tc.reasonCode, reasonCode)
		})
	}
}

// TestFinishAttempt_AlreadyAppliedAndSubmissionUnknownPersist verifies the DB
// persistence of the two queue-specific outcomes: already_applied and
// submission_unknown. These are the outcomes the queue path writes for the
// errAlreadyApplied and errSeekSubmitUnconfirmed sentinels.
func TestFinishAttempt_AlreadyAppliedAndSubmissionUnknownPersist(t *testing.T) {
	db := openAttemptTestDB(t)

	acAA := newTestAC("u1", "linkedin")
	beginAttempt(db, acAA)
	finishAttempt(db, acAA, OutcomeAlreadyApplied, "already_applied", "already_applied", "")

	var outcome string
	require.NoError(t, db.QueryRow(`SELECT outcome FROM apply_attempts WHERE id = ?`, acAA.AttemptID).Scan(&outcome))
	assert.Equal(t, "already_applied", outcome)

	acSU := newTestAC("u1", "seek")
	beginAttempt(db, acSU)
	finishAttempt(db, acSU, OutcomeSubmissionUnknown, "confirmation", "seek_submit_unconfirmed", "timeout")

	require.NoError(t, db.QueryRow(`SELECT outcome FROM apply_attempts WHERE id = ?`, acSU.AttemptID).Scan(&outcome))
	assert.Equal(t, "submission_unknown", outcome)
}
