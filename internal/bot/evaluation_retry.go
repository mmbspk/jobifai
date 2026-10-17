package bot

import (
	"context"
	"github.com/go-rod/rod"
	"github.com/rs/zerolog/log"
	"github.com/user/jobifai/internal/domain"
)

func (b *Bot) processEvaluationRetry(ctx context.Context, br *rod.Browser, j approvedJob) bool {
	// Check durable outcomes before bypassing the normal seen-cache guard.
	var unsafe int
	if err := b.cfg.DB.QueryRowContext(ctx, `SELECT
 (SELECT COUNT(*) FROM jobs_applied WHERE user_id=? AND (id=? OR link=?))+
 (SELECT COUNT(*) FROM apply_attempts WHERE user_id=? AND (job_id=? OR url=?) AND outcome IN ('submission_unknown','in_progress'))`, b.cfg.UserID, j.JobID, j.Link, b.cfg.UserID, j.JobID, j.Link).Scan(&unsafe); err != nil || unsafe > 0 {
		log.Warn().Err(err).Str("job_id", j.JobID).Str("url", j.Link).Msg("retry: evaluation blocked by unresolved or active application")
		return false
	}
	// The explicit retry supersedes a cached skip, but never an Applied entry.
	if b.seenCache == nil {
		b.seenCache = newJobSeenCache()
	}
	b.seenCache.mark(j.JobID, seenApproved)
	submitted := false
	if j.Platform == string(domain.PlatformSeek) {
		submitted = b.processSeekJobEvaluation(ctx, br, seekJob{ID: j.JobID, URL: j.Link, Title: j.Role, Company: j.Company, Location: j.Location}, true)
	} else {
		submitted = b.processLinkedInJob(ctx, br, linkedInJob{ID: j.JobID, URL: j.Link, Title: j.Role, Company: j.Company, Location: j.Location}, true)
	}
	// Only consume a queued evaluation once a durable terminal record exists.
	// LLM/DB failures without a result leave the request queued for recovery.
	var done int
	if err := b.cfg.DB.QueryRowContext(ctx, `SELECT
 (SELECT COUNT(*) FROM jobs_applied WHERE user_id=? AND (id=? OR link=?))+
 (SELECT COUNT(*) FROM jobs_skipped WHERE user_id=? AND id=?)+
 (SELECT COUNT(*) FROM jobs_pending_review WHERE user_id=? AND job_id=?)`, b.cfg.UserID, j.JobID, j.Link, b.cfg.UserID, j.JobID, b.cfg.UserID, j.JobID).Scan(&done); err == nil && done > 0 && b.seenCache.reason(j.JobID) != "in approved queue" {
		if _, err = b.cfg.DB.ExecContext(ctx, `DELETE FROM jobs_approved_queue WHERE user_id=? AND job_id=? AND needs_evaluation=1`, b.cfg.UserID, j.JobID); err != nil {
			log.Error().Err(err).Str("job_id", j.JobID).Str("url", j.Link).Msg("retry: could not consume evaluation request")
		}
	}
	return submitted
}
