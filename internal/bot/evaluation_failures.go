package bot

import (
	"context"
	"fmt"

	"github.com/user/jobifai/internal/domain"
)

// A failed evaluation never authorizes applying to the job. Callers persist the
// listing failure before this decides whether the overall run should continue.
func (b *Bot) handleEvaluationFailure(ctx context.Context, platform domain.Platform, jobID string, err error) {
	if ctx.Err() != nil || b.stopped(ctx) {
		return
	}
	// Exhausted Jobifai credits cannot fund further evaluation attempts.
	if isJobifaiQuotaExceeded(err) {
		b.abortOnLLMFailure(err)
		return
	}
	b.mu.Lock()
	if b.evaluationFailedJobs == nil {
		b.evaluationFailedJobs = make(map[string]bool)
	}
	b.evaluationFailedJobs[string(platform)+":"+jobID] = true
	count := len(b.evaluationFailedJobs)
	if count >= 3 {
		b.automaticStopReason = "3 consecutive jobs failed AI evaluation"
		b.state = domain.BotStateError
	}
	b.mu.Unlock()
	e := b.log.Warn().Str("event", "job_evaluation_failed").Str("platform", string(platform)).
		Str("job_id", jobID).Int("consecutive_failed_jobs", count).Int("stop_threshold", 3)
	if count >= 3 {
		e.Msg("bot: stopping — 3 consecutive jobs failed AI evaluation")
		b.Stop()
		return
	}
	e.Msg(fmt.Sprintf("AI evaluation failed for job; continuing search (%d/3 consecutive failed jobs)", count))
}

func (b *Bot) resetEvaluationFailures() {
	b.mu.Lock()
	b.evaluationFailedJobs = nil
	b.mu.Unlock()
}
