package bot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/rs/zerolog/log"
	"github.com/user/jobifai/internal/domain"
)

// These are recoverable listing decisions, not permanent application failures.
var errLinkedInIdentity = errors.New("job identity: LinkedIn listing could not be verified")
var errLinkedInExternal = errors.New("LinkedIn listing requires external application")
var errLinkedInUnknown = errors.New("LinkedIn application control could not be verified")

type linkedInResolution struct {
	Kind                  string `json:"kind"`
	Reason                string `json:"reason"`
	ExpectedID            string `json:"expected_id"`
	ObservedID            string `json:"observed_id"`
	IdentityVerified      bool   `json:"identity_verified"`
	Label                 string `json:"label"`
	Destination           string `json:"destination"`
	URL                   string `json:"url"`
	Description           string `json:"description"`
	DescriptionSource     string `json:"description_source"`
	DescriptionCandidates int    `json:"description_candidates"`
	DescriptionExpanded   bool   `json:"description_expanded"`
	HeaderText            string `json:"header_text"`
	Company               string `json:"company"`
	Action                string `json:"action"`
}

func linkedInExpectedID(raw string) string {
	return strings.TrimPrefix(linkedInJobPath(raw), "/jobs/view/")
}

func linkedInAppliedLoose(page *rod.Page, expectedID string, jobs ...linkedInJob) bool {
	var job linkedInJob
	if len(jobs) > 0 {
		job = jobs[0]
	}
	value, err := page.Timeout(5*time.Second).Eval(jsLinkedInAppliedLoose, expectedID, job.Title, job.Company)
	return err == nil && value.Value.Bool()
}

func linkedInSubmissionConfirmed(page *rod.Page, expectedID, company string, titles ...string) bool {
	title := ""
	if len(titles) > 0 {
		title = titles[0]
	}
	value, err := page.Timeout(5*time.Second).Eval(jsLinkedInSubmissionConfirmed, expectedID, company, title)
	if err == nil && value.Value.Bool() {
		return true
	}
	return linkedInAppliedLoose(page, expectedID, linkedInJob{Title: title, Company: company})
}

func verifyLinkedInCompletion(page *rod.Page, jobURL, company string, titles ...string) error {
	if linkedInSubmissionConfirmed(page, linkedInExpectedID(jobURL), company, titles...) {
		return nil
	}
	return verifyJobIdentity(page, linkedInJobPath(jobURL))
}

func linkedInSubmitReconciled(page *rod.Page, jobURL, company string, titles ...string) bool {
	ctx, cancel := context.WithTimeout(page.GetContext(), 45*time.Second)
	defer cancel()
	page = page.Context(ctx)
	expectedID := linkedInExpectedID(jobURL)
	job := linkedInJob{ID: expectedID, URL: jobURL, Company: company}
	if len(titles) > 0 {
		job.Title = titles[0]
	}
	if linkedInSubmissionConfirmed(page, expectedID, company, titles...) {
		return true
	}
	if r, err := resolveLinkedInJob(page, expectedID, false, job); err == nil && r.IdentityVerified && r.Kind == "already_applied" {
		return true
	}
	if linkedInAppliedLoose(page, expectedID, job) {
		return true
	}
	_ = page.Navigate(jobURL)
	_ = page.Timeout(30 * time.Second).WaitLoad()
	_ = page.Timeout(5 * time.Second).WaitStable(2 * time.Second)
	if linkedInSubmissionConfirmed(page, expectedID, company, titles...) {
		return true
	}
	r, err := resolveLinkedInJob(page, expectedID, false, job)
	if err == nil && r.IdentityVerified && r.Kind == "already_applied" {
		return true
	}
	return linkedInAppliedLoose(page, expectedID, job)
}

func waitLinkedInConfirmation(ctx context.Context, page *rod.Page, expectedID, company, jobURL string, titles ...string) error {
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if linkedInSubmissionConfirmed(page, expectedID, company, titles...) {
			return nil
		}
		job := linkedInJob{ID: expectedID, URL: jobURL, Company: company}
		if len(titles) > 0 {
			job.Title = titles[0]
		}
		if r, err := resolveLinkedInJob(page, expectedID, false, job); err == nil && r.IdentityVerified && r.Kind == "already_applied" {
			return nil
		}
		if linkedInAppliedLoose(page, expectedID, job) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			if jobURL != "" && linkedInSubmitReconciled(page, jobURL, company, titles...) {
				return nil
			}
			return errLinkedInSubmissionUnknown
		case <-time.After(300 * time.Millisecond):
		}
	}
}

// Classification and control lookup use the same resolver. Native mouse input
// rechecks that exact element after hover, so a stale SPA control cannot be used.
// ExpectedID never comes from the currently selected collection job.
func resolveLinkedInJob(page *rod.Page, expectedID string, click bool, jobs ...linkedInJob) (linkedInResolution, error) {
	var job linkedInJob
	if len(jobs) > 0 {
		job = jobs[0]
	}
	result, err := evalLinkedInJob(page, expectedID, false, false, job)
	if err != nil || !click || !result.IdentityVerified || result.Kind != "native" {
		return result, err
	}
	ctx, cancel := context.WithTimeout(page.GetContext(), 15*time.Second)
	defer cancel()
	page = page.Context(ctx)
	if err = (proto.TargetActivateTarget{TargetID: page.TargetID}).Call(page.Browser().Context(ctx)); err != nil {
		return result, fmt.Errorf("activate native application page: %w", err)
	}
	company := job.Company
	if linkedInBadgeCompany(company) {
		company = ""
	}
	button, err := page.ElementByJS(rod.Eval(jsLinkedInResolve, expectedID, false, false, job.Title, company, true))
	if err != nil {
		return result, fmt.Errorf("%w: locate native control: %v", errLinkedInIdentity, err)
	}
	if _, err = button.Eval(`()=>this.scrollIntoView({block:'center',behavior:'instant'})`); err != nil {
		return result, err
	}
	point, err := linkedInNativeClickPoint(ctx, button)
	if err != nil || point == nil {
		return result, fmt.Errorf("native control has no clickable point: %v", err)
	}
	if err = moveBoundedMouse(ctx, page, *point); err != nil {
		return result, err
	}
	// Hover can hydrate a new DOM node for the same native action. Reacquire
	// only after re-verifying the job/action, then check that fresh node before
	// the single click. Never reuse a control for another job or action kind.
	for refresh := 0; refresh < 3; refresh++ {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		result, err = evalLinkedInJob(page, expectedID, false, false, job)
		if err != nil {
			return result, err
		}
		if !result.IdentityVerified || result.Kind != "native" {
			return result, result.resolutionError()
		}
		button, err = page.ElementByJS(rod.Eval(jsLinkedInResolve, expectedID, false, false, job.Title, company, true))
		if err != nil {
			return result, fmt.Errorf("%w: reacquire native control: %v", errLinkedInIdentity, err)
		}
		point, err = linkedInNativeClickPoint(ctx, button)
		if err != nil || point == nil {
			return result, fmt.Errorf("native control moved out of reach: %v", err)
		}
		fresh, probeErr := button.Eval(`(id,title,company)=>this===(`+jsLinkedInResolve+`)(id,false,false,title,company,true)`, expectedID, job.Title, company)
		if probeErr != nil {
			return result, probeErr
		}
		if fresh == nil || !fresh.Value.Bool() {
			continue
		}
		if err = clickBoundedMouse(ctx, page, *point); err != nil {
			return result, err
		}
		result.Action = "clicked"
		return result, nil
	}
	return result, fmt.Errorf("%w: native control kept rerendering before click", errLinkedInIdentity)
}

// CDP can report an owned CSS ::after decoration as a different node. DOM hit
// testing resolves its originating element, while still rejecting unrelated
// overlays. The probe neither waits for animation frames nor reads form values.
func linkedInNativeClickPoint(ctx context.Context, button *rod.Element) (*proto.Point, error) {
	for attempt := 0; ; attempt++ {
		point, err := probeLinkedInNativeClickPoint(ctx, button)
		if err == nil || attempt == 4 || !strings.Contains(err.Error(), "control_hidden") {
			return point, err
		}
		// A modal can fade in or re-render after selection. Wait only for
		// this already-owned control to become visible; no input is sent.
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(75 * time.Millisecond):
		}
	}
}

func probeLinkedInNativeClickPoint(ctx context.Context, button *rod.Element) (*proto.Point, error) {
	state, err := button.Context(ctx).Eval(`()=>{
  if(!this.isConnected||this.disabled||this.getAttribute('aria-disabled')==='true')return {verified:false,reason:'control_unavailable'};
  for(let n=this;n;n=n.parentElement||(n.getRootNode&&n.getRootNode().host)){const s=getComputedStyle(n);if(n.hidden||n.inert||n.getAttribute('aria-hidden')==='true'||s.display==='none'||s.visibility==='hidden'||s.opacity==='0')return {verified:false,reason:'control_hidden'};}
  // Modal portals can disable pointer events on an ancestor and enable them
  // again on their controls. The live hit test below establishes whether this
  // exact control actually receives native input.
  const r=this.getBoundingClientRect(),left=Math.max(0,r.left),right=Math.min(innerWidth,r.right),top=Math.max(0,r.top),bottom=Math.min(innerHeight,r.bottom);
  if(right-left<2||bottom-top<2)return {verified:false,reason:'control_outside_viewport'};
  const x=(left+right)/2,y=(top+bottom)/2;let hit=document.elementFromPoint(x,y);
  for(let i=0;hit&&hit.shadowRoot&&i<10;i++){const next=hit.shadowRoot.elementFromPoint(x,y);if(!next||next===hit)break;hit=next;}
  for(let n=hit;n;n=n.parentElement||(n.getRootNode&&n.getRootNode().host))if(n===this)return {verified:true,x,y};
  return {verified:false,reason:'unrelated_overlay'};
 }`)
	if err != nil {
		return nil, err
	}
	if state == nil || !state.Value.Get("verified").Bool() {
		reason := "no hit-test result"
		if state != nil {
			reason = state.Value.Get("reason").String()
		}
		return nil, fmt.Errorf("native control hit test: %s", reason)
	}
	return &proto.Point{X: state.Value.Get("x").Num(), Y: state.Value.Get("y").Num()}, nil
}

func evalLinkedInJob(page *rod.Page, expectedID string, click, expand bool, job linkedInJob) (linkedInResolution, error) {
	var result linkedInResolution
	if expectedID == "" {
		return result, fmt.Errorf("%w: missing intended job ID", errLinkedInIdentity)
	}
	expectedCompany := job.Company
	if linkedInBadgeCompany(expectedCompany) {
		log.Warn().Str("job_id", expectedID).Str("stored_company", expectedCompany).
			Msg("linkedin: stored company is a badge; reading current listing metadata")
		expectedCompany = ""
	}
	value, err := page.Timeout(5*time.Second).Eval(jsLinkedInResolve, expectedID, click, expand, job.Title, expectedCompany)
	if err != nil {
		return result, fmt.Errorf("%w: browser read: %w", errLinkedInIdentity, err)
	}
	if err := json.Unmarshal([]byte(value.Value.String()), &result); err != nil {
		return result, fmt.Errorf("%w: invalid browser evidence: %v", errLinkedInIdentity, err)
	}
	return result, nil
}

// A badge stored by an older scraper is not a declared employer. The job ID
// must still match and its employer is refreshed from the verified detail pane.
var linkedInCompanyBadgePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^(?:be an early applicant|viewed|promoted|(?:re)?posted (?:today|yesterday))$`),
	regexp.MustCompile(`(?i)^(?:(?:posted|reposted)\s+)?\d+\s+(?:hour|day|week|month)s?\s+ago$`),
	regexp.MustCompile(`(?i)^(?:AUD|USD|GBP|EUR|CAD|NZD|AU\$|A\$|US\$|CA\$|NZ\$|£|€|\$)\s*\d[\d,.]*[KM]?$`),
	regexp.MustCompile(`(?i)^\d+\s+(?:company|school)\s+alumni\b`),
	regexp.MustCompile(`(?i)^\d+\s+connections?\s+works?\s+here\b`),
	regexp.MustCompile(`(?i)^actively\s+(?:reviewing|hiring)\b`),
	regexp.MustCompile(`(?i)\b(?:AUD|USD|GBP|EUR|CAD|NZD)\s*/\s*(?:yr|year|mo|month|wk|week|hr|hour)\b`),
	regexp.MustCompile(`(?i)(?:AU|US|CA|NZ|A)?[$£€]\s*\d[\d,.]*[KM]?\s*/\s*(?:yr|year|mo|month|wk|week|hr|hour)\b`),
	regexp.MustCompile(`(?i)^(?:(?:AUD|USD|GBP|EUR|CAD|NZD)\s*|(?:AU|US|CA|NZ|A)?[$£€]\s*)?\d[\d,.]*[KM]?\s*[-–—]\s*(?:(?:AUD|USD|GBP|EUR|CAD|NZD)\s*|(?:AU|US|CA|NZ|A)?[$£€]\s*)?\d[\d,.]*[KM]?\b`),
}

func linkedInBadgeCompany(company string) bool {
	value := strings.Join(strings.Fields(company), " ")
	for _, pattern := range linkedInCompanyBadgePatterns {
		if pattern.MatchString(value) {
			return true
		}
	}
	return false
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
	page = page.Context(ctx)
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
	expanded := r.DescriptionExpanded
	// Missing SDUI content may hydrate after several seconds in production.
	// Usable, stable content still returns early; cancellation remains immediate.
	deadline := time.NewTimer(8 * time.Second)
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
				Int("description_candidates", r.DescriptionCandidates).
				Str("description_source", r.DescriptionSource).
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
		// The expand control may itself arrive with a lazy-loaded description.
		r, err = evalLinkedInJob(page, expectedID, false, !expanded, job)
		if err != nil {
			return r, err
		}
		expanded = expanded || r.DescriptionExpanded
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
