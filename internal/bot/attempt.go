package bot

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
)

// AttemptOutcome is the terminal state of an application attempt.
type AttemptOutcome string

const (
	OutcomeSubmittedConfirmed AttemptOutcome = "submitted_confirmed"
	OutcomeAlreadyApplied     AttemptOutcome = "already_applied"
	OutcomeFailed             AttemptOutcome = "failed"
	OutcomeCancelled          AttemptOutcome = "cancelled"
	OutcomeTimedOut           AttemptOutcome = "timed_out"
	OutcomeBlocked            AttemptOutcome = "blocked"
	OutcomeSkipped            AttemptOutcome = "skipped"
	OutcomeSubmissionUnknown  AttemptOutcome = "submission_unknown"
	OutcomeInterrupted        AttemptOutcome = "interrupted"
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
	EntryPoint string // "runSubmit", "applyFromURL", "automated_linkedin", "automated_seek"
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

// recordAttempt inserts a completed attempt record into apply_attempts.
// It is non-fatal: a DB error is logged at Warn level but does not affect
// the caller's return path.
func recordAttempt(db *sql.DB, ac *AttemptContext, outcome AttemptOutcome, stage, reasonCode, reasonText string) {
	if db == nil || ac == nil {
		return
	}
	elapsedMS := time.Since(ac.StartedAt).Milliseconds()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.ExecContext(context.Background(),
		`INSERT OR IGNORE INTO apply_attempts
		 (id,user_id,platform,job_id,url,role,company,entry_point,outcome,stage,reason_code,reason_text,started_at,finished_at,elapsed_ms)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		ac.AttemptID, ac.UserID, ac.Platform, ac.JobID, ac.URL,
		ac.Role, ac.Company, ac.EntryPoint,
		string(outcome), stage, reasonCode, reasonText,
		ac.StartedAt.UTC().Format(time.RFC3339), now, elapsedMS,
	)
	if err != nil {
		log.Warn().Err(err).Str("attempt_id", ac.AttemptID).Str("job_id", ac.JobID).Msg("attempt: failed to record attempt outcome")
	}
}
