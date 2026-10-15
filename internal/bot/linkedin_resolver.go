package bot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/rs/zerolog/log"
	"github.com/user/jobifai/internal/domain"
)

// These are recoverable listing decisions, not permanent application failures.
var errLinkedInIdentity = errors.New("job identity: LinkedIn listing could not be verified")
var errLinkedInExternal = errors.New("LinkedIn listing requires external application")
var errLinkedInUnknown = errors.New("LinkedIn application control could not be verified")

type linkedInResolution struct {
	Kind             string `json:"kind"`
	Reason           string `json:"reason"`
	ExpectedID       string `json:"expected_id"`
	ObservedID       string `json:"observed_id"`
	IdentityVerified bool   `json:"identity_verified"`
	Label            string `json:"label"`
	Destination      string `json:"destination"`
	URL              string `json:"url"`
	Description      string `json:"description"`
	Action           string `json:"action"`
}

func linkedInExpectedID(raw string) string {
	return strings.TrimPrefix(linkedInJobPath(raw), "/jobs/view/")
}

func linkedInSubmissionConfirmed(page *rod.Page, expectedID, company string) bool {
	value, err := page.Timeout(5*time.Second).Eval(jsLinkedInSubmissionConfirmed, expectedID, company)
	return err == nil && value.Value.Bool()
}

// Resolve and click share one JS execution: a stale Go-side element cannot be
// clicked after the SPA replaces the selected job. ExpectedID never comes from
// the currently selected collection job.
func resolveLinkedInJob(page *rod.Page, expectedID string, click bool) (linkedInResolution, error) {
	var result linkedInResolution
	if expectedID == "" {
		return result, fmt.Errorf("%w: missing intended job ID", errLinkedInIdentity)
	}
	value, err := page.Timeout(5*time.Second).Eval(jsLinkedInResolve, expectedID, click)
	if err != nil {
		return result, fmt.Errorf("%w: browser read: %w", errLinkedInIdentity, err)
	}
	if err := json.Unmarshal([]byte(value.Value.String()), &result); err != nil {
		return result, fmt.Errorf("%w: invalid browser evidence: %v", errLinkedInIdentity, err)
	}
	return result, nil
}

func waitLinkedInJob(ctx context.Context, page *rod.Page, expectedID string) (linkedInResolution, error) {
	deadline := time.NewTimer(12 * time.Second)
	defer deadline.Stop()
	var result linkedInResolution
	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		var err error
		result, err = resolveLinkedInJob(page, expectedID, false)
		if err != nil {
			return result, err
		}
		if result.IdentityVerified && result.Kind != "unknown" {
			return result, nil
		}
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-deadline.C:
			return result, result.resolutionError()
		case <-time.After(300 * time.Millisecond):
		}
	}
}

func (r linkedInResolution) resolutionError() error {
	if !r.IdentityVerified {
		return fmt.Errorf("%w: expected %s, observed %s (%s)", errLinkedInIdentity, r.ExpectedID, r.ObservedID, r.Reason)
	}
	if r.Kind == "external" {
		return errLinkedInExternal
	}
	if r.Kind == "unknown" {
		return fmt.Errorf("%w: %s", errLinkedInUnknown, r.Reason)
	}
	return nil
}

func linkedInNeedsReview(err error) bool {
	if !errors.Is(err, errLinkedInSubmissionUnknown) && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		return false
	}
	return errors.Is(err, errLinkedInSubmissionUnknown) || errors.Is(err, errLinkedInIdentity) || errors.Is(err, errLinkedInExternal) || errors.Is(err, errLinkedInUnknown)
}

func logLinkedInResolution(r linkedInResolution, job linkedInJob, attemptID, stage string) {
	log.Info().Str("attempt_id", attemptID).Str("job_id", job.ID).Str("url", job.URL).
		Str("observed_url", r.URL).
		Str("stage", stage).Str("observed_job_id", r.ObservedID).
		Str("classification", r.Kind).Str("reason_code", r.Reason).
		Str("control_label", r.Label).Str("control_destination", r.Destination).
		Msg("linkedin: resolved intended job application control")
}

// Preserve an unresolved listing without incrementing attempts, erasing prepared
// documents, or granting permission to automatically submit it again.
func (b *Bot) preserveLinkedInReview(ctx context.Context, job linkedInJob, score int, reasoning string, err error) {
	if b.cfg.DB == nil {
		return
	}
	hold := "Could not verify this listing's application control. Open the original listing to review it manually."
	if errors.Is(err, errLinkedInExternal) {
		hold = "Apply on the employer's website."
	}
	if errors.Is(err, errLinkedInSubmissionUnknown) {
		hold = "Submission was attempted but could not be confirmed. Check the original listing before applying again."
	}
	pack, _ := json.Marshal(map[string]any{"hold_reason": hold, "listing_review_required": true})
	result, dbErr := b.cfg.DB.ExecContext(ctx, `INSERT INTO jobs_pending_review
		(job_id,user_id,company,role,location,platform,link,suitability_score,suitability_reasoning,easy_apply,document_refs_json,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,0,?,datetime('now'))
		ON CONFLICT(job_id) DO UPDATE SET easy_apply=0,
        document_refs_json=json_set(CASE WHEN json_valid(jobs_pending_review.document_refs_json) THEN jobs_pending_review.document_refs_json ELSE '{}' END,
        '$.hold_reason', json_extract(excluded.document_refs_json,'$.hold_reason'), '$.listing_review_required', json('true')) WHERE jobs_pending_review.user_id=excluded.user_id`,
		job.ID, b.cfg.UserID, job.Company, job.Title, job.Location, string(domain.PlatformLinkedIn), job.URL, score, reasoning, string(pack))
	if dbErr != nil {
		log.Error().Err(dbErr).Str("job_id", job.ID).Str("url", job.URL).Msg("linkedin: could not preserve listing for manual review")
		return
	}
	if count, err := result.RowsAffected(); err != nil || count == 0 {
		log.Warn().Err(err).Str("job_id", job.ID).Str("url", job.URL).Msg("linkedin: manual review row was not written; job key may belong to another user")
		return
	}
	if b.seenCache != nil {
		b.seenCache.mark(job.ID, seenPendingReview)
	}
}

var errLinkedInSubmissionUnknown = errors.New("LinkedIn submission could not be confirmed; reconcile manually before retrying")

func linkedInReviewOutcome(err error) (AttemptOutcome, string, string) {
	if errors.Is(err, errLinkedInSubmissionUnknown) {
		return OutcomeSubmissionUnknown, "confirmation", "linkedin_submit_unconfirmed"
	}
	if errors.Is(err, errLinkedInExternal) {
		return OutcomeBlocked, "classification", "external_apply"
	}
	return OutcomeBlocked, "classification", "linkedin_listing_unverified"
}
