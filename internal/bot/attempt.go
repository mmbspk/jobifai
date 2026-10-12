package bot

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
)

// AttemptOutcome is the terminal state of an application attempt.
type AttemptOutcome string

const (
	OutcomeInProgress        AttemptOutcome = "in_progress"
	OutcomeSubmittedConfirmed AttemptOutcome = "submitted_confirmed"
	OutcomeAlreadyApplied    AttemptOutcome = "already_applied"
	OutcomeFailed            AttemptOutcome = "failed"
	OutcomeCancelled         AttemptOutcome = "cancelled"
	OutcomeTimedOut          AttemptOutcome = "timed_out"
	OutcomeBlocked           AttemptOutcome = "blocked"
	OutcomeSkipped           AttemptOutcome = "skipped"
	OutcomeSubmissionUnknown AttemptOutcome = "submission_unknown"
	OutcomeInterrupted       AttemptOutcome = "interrupted"
)

// AttemptContext carries diagnostic identity for one application attempt.
// Create it at each entry point and thread it through via lazyDocGen.attemptID.
type AttemptContext struct {
	AttemptID  string
	UserID     string
	Platform   string
	JobID      string
	URL        string
	Role       string
	Company    string
	EntryPoint string // "runSubmit", "applyFromURL", "automated_linkedin", "automated_seek", "automated_queue"
	StartedAt  time.Time
}

// newAttemptContext returns an AttemptContext with a fresh v4 UUID.
func newAttemptContext(userID, platform, jobID, url, role, company, entryPoint string) *AttemptContext {
	return &AttemptContext{
		AttemptID:  newAttemptID(),
		UserID:     userID,
		Platform:   platform,
		JobID:      jobID,
		URL:        url,
		Role:       role,
		Company:    company,
		EntryPoint: entryPoint,
		StartedAt:  time.Now(),
	}
}

// newAttemptID generates a v4 UUID without importing an external package.
func newAttemptID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failure is fatal on all real platforms; return a timestamp-based fallback.
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant bits
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// beginAttempt writes an in-progress marker the moment an attempt starts.
// Call this immediately after newAttemptContext, before any browser or network I/O.
// Any attempt with outcome='in_progress' and finished_at IS NULL after a restart
// was interrupted by a crash or kill signal.
func beginAttempt(db *sql.DB, ac *AttemptContext) {
	if db == nil || ac == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := db.ExecContext(ctx,
		`INSERT OR IGNORE INTO apply_attempts
		 (id,user_id,platform,job_id,url,role,company,entry_point,outcome,started_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?)`,
		ac.AttemptID, ac.UserID, ac.Platform, ac.JobID, ac.URL,
		ac.Role, ac.Company, ac.EntryPoint, string(OutcomeInProgress),
		ac.StartedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		log.Warn().Err(err).Str("attempt_id", ac.AttemptID).Msg("attempt: failed to write in-progress marker")
	}
}

// classifyApplyOutcome maps an error from the apply/browser/navigate path to
// the most specific AttemptOutcome: cancelled for context.Canceled, timed_out
// for context.DeadlineExceeded, and failed for everything else.
func classifyApplyOutcome(err error) AttemptOutcome {
	switch {
	case errors.Is(err, context.Canceled):
		return OutcomeCancelled
	case errors.Is(err, context.DeadlineExceeded):
		return OutcomeTimedOut
	default:
		return OutcomeFailed
	}
}

// finishAttempt updates the in-progress row with the terminal outcome.
// It also refreshes job_id, url, role, company so fields that were empty at
// beginAttempt time (e.g. applyFromURL before newJobID()) are persisted correctly.
// If the UPDATE matches zero rows (beginAttempt was skipped or failed silently),
// a fallback INSERT OR IGNORE writes the terminal outcome directly.
func finishAttempt(db *sql.DB, ac *AttemptContext, outcome AttemptOutcome, stage, reasonCode, reasonText string) {
	if db == nil || ac == nil {
		return
	}
	elapsedMS := time.Since(ac.StartedAt).Milliseconds()
	now := time.Now().UTC().Format(time.RFC3339)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := db.ExecContext(ctx,
		`UPDATE apply_attempts
		 SET job_id=?, url=?, role=?, company=?,
		     outcome=?, stage=?, reason_code=?, reason_text=?,
		     finished_at=?, elapsed_ms=?
		 WHERE id=?`,
		ac.JobID, ac.URL, ac.Role, ac.Company,
		string(outcome), stage, reasonCode, reasonText,
		now, elapsedMS, ac.AttemptID,
	)
	if err != nil {
		log.Warn().Err(err).Str("attempt_id", ac.AttemptID).Str("job_id", ac.JobID).Msg("attempt: failed to update attempt outcome")
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return
	}
	log.Warn().Str("attempt_id", ac.AttemptID).Str("outcome", string(outcome)).
		Msg("attempt: in-progress row missing, writing direct fallback record")
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	_, err = db.ExecContext(ctx2,
		`INSERT OR IGNORE INTO apply_attempts
		 (id,user_id,platform,job_id,url,role,company,entry_point,outcome,stage,reason_code,reason_text,started_at,finished_at,elapsed_ms)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		ac.AttemptID, ac.UserID, ac.Platform, ac.JobID, ac.URL,
		ac.Role, ac.Company, ac.EntryPoint,
		string(outcome), stage, reasonCode, reasonText,
		ac.StartedAt.UTC().Format(time.RFC3339), now, elapsedMS,
	)
	if err != nil {
		log.Warn().Err(err).Str("attempt_id", ac.AttemptID).Msg("attempt: fallback INSERT also failed — outcome not persisted")
	}
}
