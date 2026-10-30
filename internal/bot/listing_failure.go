package bot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/rs/zerolog/log"
	"github.com/user/jobifai/internal/domain"
	"strings"
)

var errJobDescriptionMissing = errors.New("the full job description could not be read")
var errJobScorerUnavailable = errors.New("suitability evaluation is unavailable")
var errJobEvaluationFailed = errors.New("suitability evaluation could not be completed")

func listingFailureMessage(err error) (code, message string) {
	switch {
	case errors.Is(err, errLinkedInSubmissionUnknown):
		return "submission_unknown", "Submission status is unknown. Check the original listing before retrying."
	case errors.Is(err, errLinkedInIdentity):
		return "identity_unverified", "The displayed listing could not be matched to the intended job. Retry after the issue is resolved."
	case errors.Is(err, errJobDescriptionMissing):
		return "description_unavailable", "The full job description could not be read. Suitability has not been evaluated."
	case errors.Is(err, errJobScorerUnavailable):
		return "scoring_unavailable", "Suitability evaluation is unavailable. Check the AI configuration before retrying."
	case errors.Is(err, errJobEvaluationFailed):
		return "evaluation_failed", "AI evaluation could not be completed. Check the AI service before retrying."
	default:
		return "control_unrecognized", "The application button could not be verified. Retry after the issue is resolved, or open the listing manually."
	}
}

func (b *Bot) recordListingFailure(ctx context.Context, platform domain.Platform, job linkedInJob, score int, reasoning string, verdict []byte, err error) {
	code, message := listingFailureMessage(err)
	prefix := "easy apply: "
	if platform == domain.PlatformSeek {
		prefix = "seek apply: "
	}
	details, _ := json.Marshal(map[string]any{"code": code, "error": err.Error(), "intended_job_id": linkedInExpectedID(job.URL)})
	if b.cfg.DB != nil {
		result, dbErr := b.cfg.DB.ExecContext(ctx, `INSERT INTO jobs_skipped(id,user_id,platform,company,role,location,link,skip_reason,suitability_score,suitability_reasoning,halal_verdict,viewed_at,failure_code,failure_details_json)
  VALUES(?,?,?,?,?,?,?,?,?,?,?,datetime('now'),?,?) ON CONFLICT(id) DO UPDATE SET skip_reason=excluded.skip_reason,
  suitability_score=excluded.suitability_score,suitability_reasoning=excluded.suitability_reasoning,halal_verdict=excluded.halal_verdict,
  failure_code=excluded.failure_code,failure_details_json=excluded.failure_details_json,viewed_at=excluded.viewed_at WHERE jobs_skipped.user_id=excluded.user_id`, job.ID, b.cfg.UserID, string(platform), job.Company, job.Title, job.Location, job.URL, prefix+"["+code+"] "+message, skippedStoredScore(score, ""), reasoning, verdict, code, string(details))
		if dbErr != nil {
			log.Error().Err(dbErr).Str("job_id", job.ID).Str("url", redactURL(job.URL)).Msg("automation: could not persist listing failure")
			return
		}
		if count, err := result.RowsAffected(); err != nil || count == 0 {
			log.Warn().Err(err).Str("job_id", job.ID).Str("url", redactURL(job.URL)).Msg("automation: failure record not owned by this user")
			return
		}
	}
	if b.seenCache != nil {
		b.seenCache.mark(job.ID, seenSkipped)
	}
	log.Warn().Err(err).Str("job_id", job.ID).Str("url", redactURL(job.URL)).Str("platform", string(platform)).Str("reason_code", code).Msg(message)
	if code != "submission_unknown" && code != "evaluation_failed" {
		b.noteListingFailure(job.ID, code)
	}
}

// Three consecutive identical technical failures on distinct jobs stop this
// platform run. Evaluation rejections and normal external jobs reset the streak.
func (b *Bot) noteListingFailure(jobID, code string) {
	b.mu.Lock()
	if b.listingFailureCode != code {
		b.listingFailureCode = code
		b.listingFailedJobs = map[string]bool{}
	}
	b.listingFailedJobs[jobID] = true
	stop := len(b.listingFailedJobs) >= 3
	if stop {
		if b.stopCh == nil {
			b.stopCh = make(chan struct{})
		}
		b.state = domain.BotStateError
	}
	b.mu.Unlock()
	if stop {
		log.Error().Str("reason_code", code).Str("job_id", jobID).Msg("automation: repeated listing failures; run stopped for investigation")
		b.Stop()
	}
}

func (b *Bot) noteListingHealthy() {
	b.mu.Lock()
	b.listingFailureCode = ""
	b.listingFailedJobs = nil
	b.mu.Unlock()
}

func listingEvaluationError(description string, scorer JobScorer) error {
	if len([]rune(description)) < 100 {
		return fmt.Errorf("%w: insufficient description content", errJobDescriptionMissing)
	}
	if scorer == nil {
		return errJobScorerUnavailable
	}
	return nil
}

func listingScoringContext(job linkedInJob, description, header string) string {
	return fmt.Sprintf("Job title: %s\nCompany: %s\nLocation: %s\nListing information: %s\n\nJob description:\n%s", job.Title, job.Company, job.Location, header, description)
}

// Zero is a completed evaluation only on the below-threshold skip path.
// Pre-evaluation skips and failures historically used zero as a sentinel.
func skippedStoredScore(score int, reason string) any {
	if score == 0 && !strings.HasPrefix(reason, "score 0 < ") {
		return nil
	}
	return score
}
