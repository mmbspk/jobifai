package bot

// external_ats.go — external ATS handoff and Greenhouse integration (story #134).
//
// When a LinkedIn/SEEK listing uses "Apply on company website", the adaptive engine
// can follow the link to a supported external ATS (currently Greenhouse) and fill
// the form using the same snapshot → StepInterpreter → StepExecutor pipeline.
//
// Security:
//   - Origin must be greenhouse.io (strict check; no subdomain escapes).
//   - Tenant slug in the URL must fuzzy-match the declared employer before
//     any candidate data is sent — prevents mis-routed submissions.
//   - Requisition continuity is verified after each navigation step.
//   - If verification fails at any point, the attempt is aborted.
//
// Config flag: Config.EnableExternalATS must be true; when false the existing
// manual-routing behavior is preserved.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/go-rod/rod"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llm"
)

// ATSKind classifies an external apply URL by platform.
type ATSKind string

const (
	ATSKindGreenhouse ATSKind = "greenhouse"
	ATSKindUnknown    ATSKind = "unknown"
)

// greenhouse host patterns.
var (
	reGreenhouseBoards = regexp.MustCompile(`^(boards|job-boards)\.greenhouse\.io$`)
	reGreenhouseTenant = regexp.MustCompile(`^/([^/]+)/jobs/(\d+)(?:/apply)?`)
)

// classifyExternalATS identifies which ATS platform hosts the given URL.
func classifyExternalATS(rawURL string) ATSKind {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || u.User != nil {
		return ATSKindUnknown
	}
	host := strings.ToLower(u.Hostname())
	if reGreenhouseBoards.MatchString(host) {
		return ATSKindGreenhouse
	}
	return ATSKindUnknown
}

// greenhouseTenant extracts the employer slug from a Greenhouse URL path.
// Returns "" when the URL does not match the expected Greenhouse path structure.
func greenhouseTenant(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	m := reGreenhouseTenant.FindStringSubmatch(u.Path)
	if len(m) < 2 {
		return ""
	}
	return strings.ToLower(m[1])
}

// greenhouseJobID extracts the numeric job ID from a Greenhouse URL path.
func greenhouseJobID(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	m := reGreenhouseTenant.FindStringSubmatch(u.Path)
	if len(m) < 3 {
		return ""
	}
	return m[2]
}

// tenantMatchesCompany returns true when the Greenhouse tenant slug plausibly
// identifies the declared employer. The match is fuzzy: both strings are
// normalized (lowercase, non-alphanumeric removed) before substring comparison.
func tenantMatchesCompany(tenant, company string) bool {
	norm := func(s string) string {
		var b strings.Builder
		for _, r := range strings.ToLower(s) {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				b.WriteRune(r)
			}
		}
		return b.String()
	}
	nTenant := norm(tenant)
	nCompany := norm(company)
	if nTenant == "" || nCompany == "" {
		return false
	}
	if nCompany == nTenant {
		return true
	}
	stripLegalSuffix := func(value string) string {
		for {
			changed := false
			for _, suffix := range []string{"incorporated", "corporation", "limited", "corp", "inc", "ltd", "llc", "pty"} {
				if strings.HasSuffix(value, suffix) && len(value) > len(suffix)+2 {
					value = strings.TrimSuffix(value, suffix)
					changed = true
					break
				}
			}
			if !changed {
				return value
			}
		}
	}
	return stripLegalSuffix(nCompany) == stripLegalSuffix(nTenant)
}

// ── blocked state detection ───────────────────────────────────────────────────

var (
	errATSClosedJob      = errors.New("external_ats: job is closed or no longer available")
	errATSAccountWall    = errors.New("external_ats: application blocked by sign-in/account wall")
	errATSCaptcha        = errors.New("external_ats: CAPTCHA detected on application page")
	errATSTenantMismatch = errors.New("external_ats: employer tenant mismatch — refusing to send candidate data")
	errATSJobIDMismatch  = errors.New("external_ats: job requisition ID changed after redirect")
	errATSUnsupported    = errors.New("external_ats: ATS platform not supported")
	errATSAmbiguous      = errors.New("external_ats: application outcome could not be confirmed")
)

// greenhousForms that indicate a closed/expired listing.
var closedKeywords = []string{
	"this job is no longer available",
	"this job has been filled",
	"position has been filled",
	"no longer accepting applications",
	"this posting is closed",
	"job is closed",
	"expired",
}

// accountWallKeywords signal that the ATS requires the candidate to log in.
var accountWallKeywords = []string{
	"sign in to apply",
	"create an account",
	"log in to apply",
	"login to apply",
	"please sign in",
}

// captchaKeywords signal bot-protection.
var captchaKeywords = []string{
	"captcha",
	"are you a robot",
	"prove you're human",
	"cloudflare",
	"verify you are human",
}

// confirmationKeywords signal a successful submission.
var confirmationKeywords = []string{
	"thank you for applying",
	"your application has been submitted",
	"application submitted",
	"application received",
	"we've received your application",
	"we received your application",
	"successfully submitted",
	"application complete",
}

func pageTextContainsAny(text string, keywords []string) bool {
	lower := strings.ToLower(text)
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

func checkATSBlockedState(pageText string) error {
	if pageTextContainsAny(pageText, closedKeywords) {
		return errATSClosedJob
	}
	if pageTextContainsAny(pageText, accountWallKeywords) {
		return errATSAccountWall
	}
	if pageTextContainsAny(pageText, captchaKeywords) {
		return errATSCaptcha
	}
	return nil
}

func isATSConfirmed(pageText string) bool {
	return pageTextContainsAny(pageText, confirmationKeywords)
}

// ── dispatch ─────────────────────────────────────────────────────────────────

// externalATSApply is the entry point for external ATS form filling.
// It dispatches to the platform-specific runner based on the destination URL.
func (b *Bot) externalATSApply(ctx context.Context, page *rod.Page, lazy *lazyDocGen, ac *AttemptContext, externalURL string) error {
	if ac != nil {
		ctx = llm.AttachAttribution(ctx, domain.LLMCallContext{UserID: ac.UserID, JobID: ac.JobID, ApplyAttemptID: ac.AttemptID, Engine: ac.Engine, Platform: string(classifyExternalATS(externalURL))})
	}
	if lazy != nil {
		lazy.ctx = ctx
	}
	switch classifyExternalATS(externalURL) {
	case ATSKindGreenhouse:
		return b.greenhouseApply(ctx, page, lazy, ac, externalURL)
	default:
		return fmt.Errorf("%w: %s", errATSUnsupported, externalURL)
	}
}

// ── Greenhouse runner ─────────────────────────────────────────────────────────

// greenhouseApply drives the Greenhouse application form.
// It verifies tenant + requisition continuity before sending any candidate data,
// then runs the adaptive scan → interpret → execute loop.
func (b *Bot) greenhouseApply(ctx context.Context, page *rod.Page, lazy *lazyDocGen, ac *AttemptContext, externalURL string) error {
	ctx, cancel := beginAdaptiveRun(ctx)
	defer cancel()
	if ac != nil && ac.Engine == "" {
		// No explicit engine — this should only be called from the adaptive path.
		b.log.Warn().Str("attempt_id", lazy.attemptID).Msg("external_ats: engine not set on AttemptContext")
	}
	expectedTenant := greenhouseTenant(externalURL)
	expectedJobID := greenhouseJobID(externalURL)
	if expectedTenant == "" || expectedJobID == "" {
		return fmt.Errorf("external_ats: cannot parse Greenhouse URL %q", externalURL)
	}

	// Tenant must match declared employer before we navigate and expose the candidate.
	if !tenantMatchesCompany(expectedTenant, lazy.job.Company) {
		b.log.Warn().
			Str("tenant", expectedTenant).
			Str("company", lazy.job.Company).
			Str("job_id", lazy.job.ID).
			Msg("external_ats: Greenhouse tenant mismatch — skipping")
		return errATSTenantMismatch
	}
	if b.cfg.DB == nil || ac == nil || ac.AttemptID == "" {
		return ErrNoActiveAttempt
	}
	if page == nil {
		return fmt.Errorf("external_ats: application page is missing")
	}
	page, lazy.ctx = page.Context(ctx), ctx
	if err := unresolvedSubmission(b.cfg.DB, b.cfg.UserID, lazy.job.ID, lazy.job.URL, lazy.attemptID); err != nil {
		return fmt.Errorf("%w: %v", errATSAmbiguous, err)
	}
	if err := killSwitchCheck(b.cfg.EngineStore, ac); err != nil {
		return err
	}
	if err := b.checkAdaptiveSideEffect(); err != nil {
		return err
	}
	if ac != nil && b.cfg.DB != nil {
		identity, err := json.Marshal(map[string]string{"platform": "greenhouse", "tenant": expectedTenant, "job_id": expectedJobID, "url": externalURL})
		if err != nil {
			return err
		}
		result, err := b.cfg.DB.ExecContext(ctx, `UPDATE apply_attempts SET external_identity_json=? WHERE id=? AND user_id=? AND outcome='in_progress' AND stage<>'submit_dispatch'`, string(identity), ac.AttemptID, ac.UserID)
		if err != nil {
			return err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrNoActiveAttempt
		}
		ac.ExternalATS = ATSKindGreenhouse
	}

	b.log.Info().
		Str("tenant", expectedTenant).
		Str("gh_job_id", expectedJobID).
		Str("attempt_id", lazy.attemptID).
		Msg("external_ats: navigating to Greenhouse form")

	// Navigate to the Greenhouse apply page.
	if err := page.Timeout(30 * time.Second).Navigate(externalURL); err != nil {
		return fmt.Errorf("external_ats: navigate to Greenhouse: %w", err)
	}
	if err := page.Timeout(30 * time.Second).WaitLoad(); err != nil {
		return fmt.Errorf("external_ats: wait load: %w", err)
	}

	// Verify the landed URL still belongs to the expected tenant and job
	// (protects against open-redirect chains that land on a different employer).
	landedURL, err := page.Eval(`() => window.location.href`)
	if err != nil || landedURL == nil {
		return fmt.Errorf("external_ats: landed application identity unreadable")
	}
	{
		landed := landedURL.Value.String()
		if classifyExternalATS(landed) != ATSKindGreenhouse {
			return fmt.Errorf("external_ats: redirect left Greenhouse domain: %q", landed)
		}
		landedTenant := greenhouseTenant(landed)
		landedJobID := greenhouseJobID(landed)
		if landedTenant != expectedTenant {
			return fmt.Errorf("%w: expected %q, got %q", errATSTenantMismatch, expectedTenant, landedTenant)
		}
		if landedJobID != expectedJobID {
			return fmt.Errorf("%w: expected %q, got %q", errATSJobIDMismatch, expectedJobID, landedJobID)
		}
	}

	// Check for blocked states before touching any form.
	pageText, _ := page.Eval(`() => document.body ? document.body.innerText : ''`)
	text := ""
	if pageText != nil {
		text = pageText.Value.String()
	}
	if err := checkATSBlockedState(text); err != nil {
		return err
	}

	// Already submitted (redirected to confirmation from a prior run)?
	if greenhouseSubmissionConfirmed(page.Context(ctx), expectedTenant, expectedJobID) {
		b.log.Info().Str("attempt_id", lazy.attemptID).Msg("external_ats: already-confirmed page — treating as successful prior submission")
		return errAlreadyApplied
	}

	b.log.Info().Str("tenant", expectedTenant).Str("attempt_id", lazy.attemptID).Msg("external_ats: Greenhouse form loaded, entering step loop")

	// Set up the adaptive pipeline for the Greenhouse form.
	profile := b.currentProfile()
	si := &StepInterpreter{
		KnownMissing: b.knownMissingChecker(lazy),
		LLM:          boundFormLLM(b.cfg.AdaptiveLLM),
		Mapping:      b.cfg.AdaptiveMapping,
	}
	guard := &SubmitGuard{
		DB:        b.cfg.DB,
		AttemptID: lazy.attemptID,
		UserID:    b.cfg.UserID,
	}
	se := NewStepExecutor(DefaultStepExecBounds(), guard)
	se.BeforeSideEffect = func(ctx context.Context) error {
		if err := b.ensureAnswerFactsCurrent(lazy); err != nil {
			return err
		}
		if err := b.checkAdaptiveSideEffect(); err != nil {
			return err
		}
		if err := killSwitchCheck(b.cfg.EngineStore, ac); err != nil {
			return err
		}
		current := currentPageURL(page.Context(ctx))
		if classifyExternalATS(current) != ATSKindGreenhouse || greenhouseTenant(current) != expectedTenant || greenhouseJobID(current) != expectedJobID {
			return fmt.Errorf("external_ats: application identity changed before an action")
		}
		return nil
	}
	filler := &greenhouseRodFiller{
		page:           page,
		bot:            b,
		lazy:           lazy,
		expectedTenant: expectedTenant,
		expectedJobID:  expectedJobID,
	}

	const maxFormSteps = 20
	for stepIdx := 0; stepIdx < maxFormSteps; stepIdx++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Scan the current form page.
		fields, scanPage, _ := b.scanAdaptiveStep(ctx, page)
		if scanPage != nil {
			filler.page = scanPage
		}

		stepTitle := fmt.Sprintf("greenhouse_step_%d", stepIdx)
		if ti, terr := page.Eval(`() => { const h = document.querySelector('h1,h2,h3'); return h ? h.textContent.trim() : '' }`); terr == nil && ti.Value.String() != "" {
			stepTitle = ti.Value.String()
		}

		snap := captureStepSnapshot(fields, nil, "greenhouse", stepIdx, stepTitle)
		scopeJobDependentFields(&snap, lazy.job)
		lazy.applyCaps(capsFromFormFields(fields))
		snap.EmployerKey = expectedTenant

		interp, err := si.InterpretStep(ctx, snap, profile)
		if err != nil {
			return fmt.Errorf("external_ats: interpret step %d: %w", stepIdx, err)
		}
		if interp.Paused {
			recordAdaptiveProposals(ctx, b.cfg.AdaptiveMapping, snap, interp)
			if _, err := b.prepareAnswerReviews(ctx, lazy, snap, interp, profile); err != nil {
				return err
			}
			return fmt.Errorf("%w: step %d (%s)", errAdaptiveQuestionReview, stepIdx, interp.PauseReason)
		}

		recordAdaptiveProposals(ctx, b.cfg.AdaptiveMapping, snap, interp)
		reviewIDs, reviewErr := b.prepareAnswerReviews(ctx, lazy, snap, interp, profile)
		if reviewErr != nil {
			return reviewErr
		}
		se.AfterFieldFill = b.answerFillRecorder(reviewIDs)
		result, err := se.ExecuteStep(ctx, snap, interp, filler)
		invalidateFailedMappings(ctx, b.cfg.AdaptiveMapping, snap, interp, result)
		if err != nil && result.Outcome != StepOutcomeSubmitted {
			return fmt.Errorf("external_ats: execute step %d: %w", stepIdx, err)
		}

		switch result.Outcome {
		case StepOutcomeSubmitted:
			if !waitAdaptiveConfirmation(ctx, func() bool { return greenhouseSubmissionConfirmed(page.Context(ctx), expectedTenant, expectedJobID) }) {
				b.log.Warn().Str("attempt_id", lazy.attemptID).Msg("external_ats: submit clicked but no confirmation detected")
				return errATSAmbiguous
			}
			// Persist only after confirmation.
			writeConfirmedStep(ctx, b.cfg.AdaptiveMapping, snap, interp, result, true)
			b.log.Info().Str("attempt_id", lazy.attemptID).Int("step", stepIdx).Msg("external_ats: Greenhouse application confirmed")
			return nil
		case StepOutcomeAdvanced:
			// Blocked-state gate BEFORE persistence. NavigateNext already refuses to
			// confirm a blocked destination, but we re-check the landed page here so
			// that an account wall / closed / captcha page that somehow advanced the
			// form marker still yields ZERO accepted_by_form writes for this step.
			pt, _ := page.Eval(`() => document.body ? document.body.innerText : ''`)
			if pt != nil {
				if err := checkATSBlockedState(pt.Value.String()); err != nil {
					return err
				}
			}
			// NavigateNext verified a distinct, in-application step and a non-blocked
			// destination — persist only the refs the executor reported as filled.
			writeConfirmedStep(ctx, b.cfg.AdaptiveMapping, snap, interp, result, false)
		case StepOutcomePaused:
			return fmt.Errorf("external_ats: step %d paused", stepIdx)
		default:
			return fmt.Errorf("external_ats: step %d unexpected outcome: %s", stepIdx, result.Outcome)
		}
	}

	return fmt.Errorf("external_ats: exceeded max form steps (%d)", maxFormSteps)
}

func greenhouseSubmissionConfirmed(page *rod.Page, tenant, jobID string) bool {
	current := currentPageURL(page)
	if classifyExternalATS(current) != ATSKindGreenhouse || greenhouseTenant(current) != tenant || greenhouseJobID(current) != jobID {
		return false
	}
	result, err := page.Eval(`()=>{` + jsGreenhouseFormScopeFn + `
  const containers=[...document.querySelectorAll('#application_confirmation,#application-confirmation,.application-confirmation,[data-application-status="confirmed"]')].filter(greenhouseElementVisible);
  if(containers.length===1)return containers[0].innerText;
  if(resolveGreenhouseApplicationForm())return '';
  const headings=[...document.querySelectorAll('h1,h2,[role="heading"]')].filter(greenhouseElementVisible).map(el=>el.textContent.trim()).filter(text=>/^(thank you for applying[.!]?|application (submitted|received)[.!]?|your application (has been|was) (submitted|received)[.!]?)$/i.test(text));
  return headings.length===1?headings[0]:'';
 }`)
	return err == nil && result != nil && isATSConfirmed(result.Value.String())
}

// ── greenhouseRodFiller ───────────────────────────────────────────────────────

// greenhouseRodFiller implements FieldFiller for Greenhouse HTML forms.
// Greenhouse uses standard HTML form elements so the LinkedIn fill helpers work
// without modification; navigation is different (single submit button).
//
// expectedTenant/expectedJobID anchor the filler to the one application that was
// verified in greenhouseApply before any candidate data was sent. NavigateNext
// refuses to treat a post-click page as the next step unless it still provably
// belongs to that application — a bare URL change is never sufficient.
type greenhouseRodFiller struct {
	page           *rod.Page
	bot            *Bot
	lazy           *lazyDocGen
	expectedTenant string
	expectedJobID  string
}

func (f *greenhouseRodFiller) Fill(ctx context.Context, field SnapField, res FieldResolution) FieldFillResult {
	if res.Action == ActionSkip {
		return FieldFillResult{Skipped: true}
	}
	if res.Action == ActionUpload {
		return f.bot.fillAdaptiveDocument(ctx, f.page, f.lazy, field)
	}

	return fillAdaptiveControl(ctx, f.page, field, res.Value)
}

// currentPageURL returns the current URL of a page, or "" on error.
func currentPageURL(page *rod.Page) string {
	res, err := page.Eval(`() => window.location.href`)
	if err != nil || res == nil {
		return ""
	}
	return res.Value.String()
}

// This scope is shared by scanning, filling and transition observation. A large
// unrelated form is not evidence of an active application. Multiple application
// forms are ambiguous and must not be selected by document order or field count.
const jsGreenhouseFormScopeFn = `
function greenhouseElementVisible(el) {
 if (!el || !el.isConnected) return false;
 const rect = el.getBoundingClientRect();
 if (rect.width <= 0 || rect.height <= 0) return false;
 for (let node = el; node; node = node.parentElement) {
  const style = getComputedStyle(node);
  if (style.display === 'none' || style.visibility === 'hidden' || style.visibility === 'collapse'
      || Number(style.opacity) === 0 || node.hidden || node.hasAttribute('inert')
      || node.getAttribute('aria-hidden') === 'true') return false;
 }
 return true;
}
function resolveGreenhouseApplicationForm() {
 const forms = [...document.querySelectorAll(
  'form#application_form, form#application-form, form[data-testid="application-form"], form[data-application="greenhouse"]'
 )].filter(greenhouseElementVisible);
 return forms.length === 1 ? forms[0] : null;
}
function greenhouseAction(kind) {
 const form = resolveGreenhouseApplicationForm();
 if (!form) return null;
 const candidates = [...form.querySelectorAll('button, input[type="submit"], [role="button"]')].filter(el => {
  if (!greenhouseElementVisible(el) || el.disabled || el.getAttribute('aria-disabled') === 'true') return false;
  const label = (el.getAttribute('aria-label') || el.textContent || el.value || '').trim().toLowerCase();
  if (kind === 'next') return /^(next|continue|save and continue|review|review application)$/.test(label);
  return /^(submit|submit application|apply|apply now|send application)$/.test(label);
 });
 return candidates.length === 1 ? candidates[0] : null;
}
`

// greenhouseStepMarker uses the current controls' meanings, excluding cosmetic
// IDs, layout order, answers and headings. Empty forms do not prove advancement.
func greenhouseStepMarker(page *rod.Page) string {
	raw, err := page.Eval(jsScanFields, map[string]any{"includeAnsweredFields": true, "includeAnsweredRadios": true})
	if err != nil || raw == nil {
		return ""
	}
	var fields []formField
	if err := json.Unmarshal([]byte(raw.Value.String()), &fields); err != nil || len(fields) == 0 {
		return ""
	}
	snapshot := captureStepSnapshot(fields, nil, "greenhouse", 0, "")
	if snapshot.Truncated {
		return ""
	}
	signatures := make([]string, len(snapshot.Fields))
	for i, field := range snapshot.Fields {
		// Headings and hints can change on validation or a cosmetic rerender.
		// They constrain mapping reuse but do not alone prove step advancement.
		field.Group, field.HelpText = "", ""
		signatures[i] = field.FieldSignature("greenhouse")
	}
	sort.Strings(signatures)
	return strings.Join(signatures, ";")
}

// destinationInApplication reports whether the page now loaded in f.page provably
// belongs to the anchored application (f.expectedTenant / f.expectedJobID).
//
// Identity must be established from the URL: greenhouseTenant and greenhouseJobID
// parse the same path structure, so a URL that carries a tenant slug always carries
// a job ID too and vice versa. Requiring both IDs from the URL eliminates the
// need for — and the risk of — DOM fallback: a matching job ID in the form DOM
// cannot prove the tenant, so we do not use it here.
func (f *greenhouseRodFiller) destinationInApplication(rawURL string) bool {
	if classifyExternalATS(rawURL) != ATSKindGreenhouse {
		return false
	}
	urlTenant := greenhouseTenant(rawURL)
	urlJob := greenhouseJobID(rawURL)

	// Both IDs must be present in the URL — a URL that omits either has no
	// provable application identity.
	if urlTenant == "" || urlJob == "" {
		return false
	}
	if f.expectedTenant != "" && urlTenant != f.expectedTenant {
		return false
	}
	if f.expectedJobID != "" && urlJob != f.expectedJobID {
		return false
	}
	return true
}

// destinationBlocked returns the blocked-state error for the currently loaded page
// (account wall / closed / captcha), or nil when the page is a normal form step.
func (f *greenhouseRodFiller) destinationBlocked() error {
	txt, err := f.page.Eval(`() => document.body ? document.body.innerText : ''`)
	if err != nil || txt == nil {
		return fmt.Errorf("external_ats: destination state unreadable")
	}
	return checkATSBlockedState(txt.Value.String())
}

func (f *greenhouseRodFiller) NavigateNext(ctx context.Context) (bool, error) {
	page := f.page.Context(ctx)
	// Anchor the step we are leaving BEFORE the click.
	// If the current form step is not identifiable (no visible controls), we cannot
	// know what we are advancing FROM — leave the transition unconfirmed so that
	// no mappings are written based on ambiguous provenance.
	prevMarker := greenhouseStepMarker(page)
	if prevMarker == "" {
		return false, fmt.Errorf("external_ats: cannot identify current form step — leaving transition unconfirmed")
	}
	prevURL := currentPageURL(page)

	res, err := page.Eval(`() => {` + jsGreenhouseFormScopeFn + `
		const btn = greenhouseAction('next');
		if (!btn || btn.disabled) return false;
		btn.click();
		return true;
	}`)
	if err != nil {
		return false, fmt.Errorf("external_ats: NavigateNext eval: %w", err)
	}
	if !res.Value.Bool() {
		return false, nil
	}

	// A genuine advance requires ALL of: (1) a DISTINCT, readable form step now
	// showing — a changed URL or changed query string with the same step does not
	// count; (2) the destination still provably belongs to THIS application; and
	// (3) the destination is not a blocked page (account wall / closed / captcha).
	// Any of these failing yields zero accepted_by_form writes for the prior step.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		time.Sleep(200 * time.Millisecond)

		newMarker := greenhouseStepMarker(page)
		if newMarker == "" || newMarker == prevMarker {
			// Same step still showing, or form not yet readable — keep waiting.
			continue
		}

		newURL := currentPageURL(page)
		if newURL == "" {
			// Distinct markup but URL unreadable — cannot confirm identity.
			continue
		}
		if classifyExternalATS(newURL) != ATSKindGreenhouse {
			return false, fmt.Errorf("external_ats: navigation left Greenhouse domain: %q", newURL)
		}
		if !f.destinationInApplication(newURL) {
			return false, fmt.Errorf(
				"external_ats: navigation left the anchored application (tenant=%q job=%q) → %q",
				f.expectedTenant, f.expectedJobID, newURL)
		}
		if blockErr := f.destinationBlocked(); blockErr != nil {
			return false, blockErr
		}
		return true, nil
	}
	return false, fmt.Errorf(
		"external_ats: step did not advance after click — no distinct in-application form step within 5s (url=%q)",
		prevURL)
}

func (f *greenhouseRodFiller) IsSubmitReady(ctx context.Context) (bool, error) {
	page := f.page.Context(ctx)
	res, err := page.Eval(`() => {` + jsGreenhouseFormScopeFn + `
		const btn = greenhouseAction('submit');
		return !!(btn && !btn.disabled);
	}`)
	if err != nil {
		return false, nil
	}
	return res.Value.Bool(), nil
}

func (f *greenhouseRodFiller) ClickSubmit(ctx context.Context) error {
	page := f.page.Context(ctx)
	res, err := page.Eval(`() => {` + jsGreenhouseFormScopeFn + `
		const btn = greenhouseAction('submit');
		if (!btn || btn.disabled) return false;
		btn.click();
		return true;
	}`)
	if err != nil {
		return fmt.Errorf("external_ats: ClickSubmit eval: %w", err)
	}
	if !res.Value.Bool() {
		return errors.New("external_ats: submit button not found or disabled")
	}
	return nil
}
