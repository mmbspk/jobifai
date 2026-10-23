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
	HeaderText       string `json:"header_text"`
	Action           string `json:"action"`
}

func linkedInExpectedID(raw string) string {
	return strings.TrimPrefix(linkedInJobPath(raw), "/jobs/view/")
}

func linkedInSubmissionConfirmed(page *rod.Page, expectedID, company string) bool {
	value, err := page.Timeout(5*time.Second).Eval(jsLinkedInSubmissionConfirmed, expectedID, company)
	return err == nil && value.Value.Bool()
}

func verifyLinkedInCompletion(page *rod.Page, jobURL, company string) error {
	if linkedInSubmissionConfirmed(page, linkedInExpectedID(jobURL), company) {
		return nil
	}
	return verifyJobIdentity(page, linkedInJobPath(jobURL))
}

func waitLinkedInConfirmation(ctx context.Context, page *rod.Page, expectedID, company string) error {
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if linkedInSubmissionConfirmed(page, expectedID, company) {
			return nil
		}
		if err := verifyJobIdentity(page, "/jobs/view/"+expectedID); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errLinkedInSubmissionUnknown
		case <-time.After(300 * time.Millisecond):
		}
	}
}

// Resolve and click share one JS execution: a stale Go-side element cannot be
// clicked after the SPA replaces the selected job. ExpectedID never comes from
// the currently selected collection job.
func resolveLinkedInJob(page *rod.Page, expectedID string, click bool, jobs ...linkedInJob) (linkedInResolution, error) {
	var job linkedInJob
	if len(jobs) > 0 {
		job = jobs[0]
	}
	return evalLinkedInJob(page, expectedID, click, false, job)
}

func evalLinkedInJob(page *rod.Page, expectedID string, click, expand bool, job linkedInJob) (linkedInResolution, error) {
	var result linkedInResolution
	if expectedID == "" {
		return result, fmt.Errorf("%w: missing intended job ID", errLinkedInIdentity)
	}
	value, err := page.Timeout(5*time.Second).Eval(jsLinkedInResolve, expectedID, click, expand, job.Title, job.Company)
	if err != nil {
		return result, fmt.Errorf("%w: browser read: %w", errLinkedInIdentity, err)
	}
	if err := json.Unmarshal([]byte(value.Value.String()), &result); err != nil {
		return result, fmt.Errorf("%w: invalid browser evidence: %v", errLinkedInIdentity, err)
	}
	return result, nil
}

func waitLinkedInJob(ctx context.Context, page *rod.Page, expectedID string, jobs ...linkedInJob) (linkedInResolution, error) {
	deadline := time.NewTimer(12 * time.Second)
	defer deadline.Stop()
	var result linkedInResolution
	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		var err error
		result, err = resolveLinkedInJob(page, expectedID, false, jobs...)
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

func readLinkedInDescription(ctx context.Context, page *rod.Page, job linkedInJob) (linkedInResolution, error) {
	const minDescLen = 100
	expectedID := linkedInExpectedID(job.URL)
	r, err := evalLinkedInJob(page, expectedID, false, true, job)
	if err != nil {
		return r, err
	}
	if !r.IdentityVerified {
		return r, r.resolutionError()
	}
	// Preserve the pre-settle snapshot as a safety net; the expand click may
	// trigger a React re-render (element temporarily absent) or load additional
	// content asynchronously. Always wait at least one retry cycle so the
	// expanded description has a chance to arrive, then return the best verified
	// content seen across all reads.
	//
	// Note: a collapsed preview can exceed the minimum usable length, so the
	// first read is never sufficient on its own — expansion must be given time
	// to settle before we select the final description.
	bestDesc := r.Description
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	wait := 400 * time.Millisecond
	lastDescLen := -1
	stableCount := 0
	for {
		select {
		case <-ctx.Done():
			return r, ctx.Err()
		case <-deadline.C:
			if r.IdentityVerified && len([]rune(bestDesc)) >= minDescLen {
				r.Description = bestDesc
				return r, nil
			}
			log.Warn().
				Str("job_id", job.ID).
				Str("url", job.URL).
				Int("best_desc_len", len([]rune(bestDesc))).
				Bool("last_identity_verified", r.IdentityVerified).
				Str("last_reason", r.Reason).
				Str("last_observed_id", r.ObservedID).
				Msg("linkedin: description content not available within deadline after expand")
			if !r.IdentityVerified {
				return r, fmt.Errorf("%w: identity lost at deadline (best=%d chars)",
					errLinkedInIdentity, len([]rune(bestDesc)))
			}
			return r, fmt.Errorf("%w: description content not available after expand (best=%d chars)",
				errJobDescriptionMissing, len([]rune(bestDesc)))
		case <-time.After(wait):
			wait = 200 * time.Millisecond
		}
		r, err = resolveLinkedInJob(page, expectedID, false, job)
		if err != nil {
			return r, err
		}
		// Genuine navigation drift — abort immediately.
		if r.ObservedID != "" && r.ObservedID != expectedID {
			return r, r.resolutionError()
		}
		if !r.IdentityVerified {
			// DOM still mid-rerender — keep waiting, but reset stability counter.
			stableCount = 0
			continue
		}
		// Identity valid. Update best and track description stability.
		currLen := len([]rune(r.Description))
		if currLen > len([]rune(bestDesc)) {
			bestDesc = r.Description
		}
		if currLen == lastDescLen {
			stableCount++
		} else {
			stableCount = 0
		}
		lastDescLen = currLen
		// Require two consecutive reads with identical description length before
		// declaring expansion settled. A single usable read may still represent
		// content in flight.
		if currLen >= minDescLen && stableCount >= 2 {
			r.Description = bestDesc
			return r, nil
		}
	}
}

// Expanding the description may replace the header. Manual URL application
// must use that refreshed classification, never interpret unknown as external.
func (b *Bot) readLinkedInURLListing(ctx context.Context, page *rod.Page, job linkedInJob, ac *AttemptContext) (linkedInResolution, error) {
	r, err := readLinkedInDescription(ctx, page, job)
	if err == nil && r.IdentityVerified && r.Kind == "unknown" {
		// Allow an ordinary header rerender to settle before declaring that the
		// application control cannot be recognized.
		r, err = waitLinkedInJob(ctx, page, linkedInExpectedID(job.URL), job)
	}
	if err == nil && r.Kind == "already_applied" {
		finishAttempt(b.cfg.DB, ac, OutcomeAlreadyApplied, "classification", "already_applied", "")
		return r, ErrAlreadyApplied
	}
	if err == nil && r.Kind != "native" && r.Kind != "external" {
		err = fmt.Errorf("%w: refreshed control: %s", errLinkedInUnknown, r.Reason)
	}
	if err != nil {
		b.recordListingFailure(ctx, domain.PlatformLinkedIn, job, 0, "", nil, err)
		outcome := OutcomeBlocked
		if !linkedInNeedsReview(err) {
			outcome = classifyApplyOutcome(err)
		}
		finishAttempt(b.cfg.DB, ac, outcome, "extraction", "listing_unverified", err.Error())
	}
	return r, err
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
	log.Info().Str("attempt_id", attemptID).Str("job_id", job.ID).Str("url", redactURL(job.URL)).
		Str("observed_url", redactURL(r.URL)).
		Str("stage", stage).Str("observed_job_id", r.ObservedID).
		Str("classification", r.Kind).Str("reason_code", r.Reason).
		Str("control_label", r.Label).Str("control_destination", r.Destination).
		Msg("linkedin: resolved intended job application control")
}

// Preserve an unresolved listing without incrementing attempts, erasing prepared
// documents, or granting permission to automatically submit it again.
func (b *Bot) preserveLinkedInReview(ctx context.Context, job linkedInJob, score int, reasoning string, err error) {
	b.preserveLinkedInReviewWithVerdict(ctx, job, score, reasoning, nil, err)
}

func (b *Bot) preserveLinkedInReviewWithVerdict(ctx context.Context, job linkedInJob, score int, reasoning string, halalVerdict []byte, err error) {
	if !errors.Is(err, errLinkedInExternal) {
		b.recordListingFailure(ctx, domain.PlatformLinkedIn, job, score, reasoning, halalVerdict, err)
		return
	}
	if b.cfg.DB == nil {
		return
	}
	hold := "Could not verify this listing's application control. Open the original listing to review it manually."
	if errors.Is(err, errLinkedInIdentity) {
		hold = "Could not verify that the displayed job is the intended listing. Open the original listing to check it."
	}
	if errors.Is(err, errLinkedInExternal) {
		hold = "Apply on the employer's website."
	}
	if errors.Is(err, errLinkedInSubmissionUnknown) {
		hold = "Submission was attempted but could not be confirmed. Check the original listing before applying again."
	}
	pack, _ := json.Marshal(map[string]any{"hold_reason": hold, "listing_review_required": true, "listing_review_diagnostic": err.Error()})
	result, dbErr := b.cfg.DB.ExecContext(ctx, `INSERT INTO jobs_pending_review
		(job_id,user_id,company,role,location,platform,link,suitability_score,suitability_reasoning,easy_apply,document_refs_json,halal_verdict,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,0,?,?,datetime('now'))
		ON CONFLICT(job_id) DO UPDATE SET easy_apply=0,
        document_refs_json=json_set(CASE WHEN json_valid(jobs_pending_review.document_refs_json) THEN jobs_pending_review.document_refs_json ELSE '{}' END,
        '$.hold_reason', json_extract(excluded.document_refs_json,'$.hold_reason'), '$.listing_review_required', json('true'),
        '$.listing_review_diagnostic', json_extract(excluded.document_refs_json,'$.listing_review_diagnostic')) WHERE jobs_pending_review.user_id=excluded.user_id`,
		job.ID, b.cfg.UserID, job.Company, job.Title, job.Location, string(domain.PlatformLinkedIn), job.URL, score, reasoning, string(pack), string(halalVerdict))
	if dbErr != nil {
		log.Error().Err(dbErr).Str("job_id", job.ID).Str("url", redactURL(job.URL)).Msg("linkedin: could not preserve listing for manual review")
		return
	}
	if count, err := result.RowsAffected(); err != nil || count == 0 {
		log.Warn().Err(err).Str("job_id", job.ID).Str("url", redactURL(job.URL)).Msg("linkedin: manual review row was not written; job key may belong to another user")
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
