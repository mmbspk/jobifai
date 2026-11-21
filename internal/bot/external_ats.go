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
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/go-rod/rod"
)

// ATSKind classifies an external apply URL by platform.
type ATSKind string

const (
	ATSKindGreenhouse ATSKind = "greenhouse"
	ATSKindUnknown    ATSKind = "unknown"
)

// greenhouse host patterns.
var (
	reGreenhouseBoards  = regexp.MustCompile(`^(boards|job-boards)\.greenhouse\.io$`)
	reGreenhouseTenant  = regexp.MustCompile(`^/([^/]+)/jobs/(\d+)(?:/apply)?`)
)

// classifyExternalATS identifies which ATS platform hosts the given URL.
func classifyExternalATS(rawURL string) ATSKind {
	u, err := url.Parse(rawURL)
	if err != nil {
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
	return strings.Contains(nCompany, nTenant) || strings.Contains(nTenant, nCompany)
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
	if err == nil {
		landed := landedURL.Value.String()
		if classifyExternalATS(landed) != ATSKindGreenhouse {
			return fmt.Errorf("external_ats: redirect left Greenhouse domain: %q", landed)
		}
		landedTenant := greenhouseTenant(landed)
		landedJobID := greenhouseJobID(landed)
		if landedTenant != "" && landedTenant != expectedTenant {
			return fmt.Errorf("%w: expected %q, got %q", errATSTenantMismatch, expectedTenant, landedTenant)
		}
		if landedJobID != "" && landedJobID != expectedJobID {
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
	if isATSConfirmed(text) {
		b.log.Info().Str("attempt_id", lazy.attemptID).Msg("external_ats: already-confirmed page — treating as successful prior submission")
		return nil
	}

	b.log.Info().Str("tenant", expectedTenant).Str("attempt_id", lazy.attemptID).Msg("external_ats: Greenhouse form loaded, entering step loop")

	// Set up the adaptive pipeline for the Greenhouse form.
	profile := b.currentProfile()
	si := &StepInterpreter{
		LLM:     b.cfg.AdaptiveLLM,
		Mapping: b.cfg.AdaptiveMapping,
	}
	guard := &SubmitGuard{
		DB:        b.cfg.DB,
		AttemptID: lazy.attemptID,
		UserID:    b.cfg.UserID,
	}
	se := NewStepExecutor(DefaultStepExecBounds(), guard)
	filler := &greenhouseRodFiller{
		page:           page,
		bot:            b,
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

		interp, err := si.InterpretStep(ctx, snap, profile)
		if err != nil {
			return fmt.Errorf("external_ats: interpret step %d: %w", stepIdx, err)
		}
		if interp.Paused {
			return fmt.Errorf("external_ats: step %d paused: %s", stepIdx, interp.PauseReason)
		}

		result, err := se.ExecuteStep(ctx, snap, interp, filler)
		if err != nil && result.Outcome != StepOutcomeSubmitted {
			return fmt.Errorf("external_ats: execute step %d: %w", stepIdx, err)
		}

		switch result.Outcome {
		case StepOutcomeSubmitted:
			// Verify confirmation page before persisting mappings.
			time.Sleep(1 * time.Second)
			confirmText, _ := page.Eval(`() => document.body ? document.body.innerText : ''`)
			ct := ""
			if confirmText != nil {
				ct = confirmText.Value.String()
			}
			if !isATSConfirmed(ct) {
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
	expectedTenant string
	expectedJobID  string
}

func (f *greenhouseRodFiller) Fill(ctx context.Context, field SnapField, res FieldResolution) FieldFillResult {
	if res.Action == ActionSkip {
		return FieldFillResult{Skipped: true}
	}
	if res.Action == ActionUpload {
		idx, err := refIndex(field.Ref)
		if err != nil {
			return FieldFillResult{Err: err}
		}
		result, evalErr := f.page.Eval(jsFillFileNth, idx)
		if evalErr != nil || result == nil || !result.Value.Bool() {
			return FieldFillResult{Err: fmt.Errorf("external_ats: file upload slot not found (ref %s)", field.Ref)}
		}
		return FieldFillResult{Filled: true}
	}

	var fieldTypeParm string
	switch field.Type {
	case SnapTypeRadio:
		fieldTypeParm = "radio"
	case SnapTypeSelect:
		fieldTypeParm = "select"
	default:
		fieldTypeParm = "text"
	}
	result, err := f.page.Eval(jsFillByQuestionAnswer, field.Label, res.Value, fieldTypeParm, field.Group, field.ControlID)
	if err != nil {
		return FieldFillResult{Err: fmt.Errorf("external_ats: fill field %s (%q): %w", field.Ref, field.Label, err)}
	}
	if !result.Value.Bool() {
		return FieldFillResult{Err: fmt.Errorf("external_ats: fill returned false for field %s (%q)", field.Ref, field.Label)}
	}
	return FieldFillResult{Filled: true}
}

// currentPageURL returns the current URL of a page, or "" on error.
func currentPageURL(page *rod.Page) string {
	res, err := page.Eval(`() => window.location.href`)
	if err != nil || res == nil {
		return ""
	}
	return res.Value.String()
}

// greenhouseStepMarker returns a fingerprint of the active Greenhouse form step,
// derived from the ordered identifiers of the *visible* form controls. It
// deliberately ignores the page heading, field *values*, inline *validation text*,
// and URL *query parameters* — none of which indicate a real step transition. Only
// controls that are actually rendered (not type="hidden" and not CSS-hidden via
// offsetParent) contribute. The form itself must be in a visible container.
// Returns "" when the active form step cannot be identified (an unidentifiable
// step is never evidence of advancement, neither before nor after clicking Next).
func greenhouseStepMarker(page *rod.Page) string {
	res, err := page.Eval(`() => {
		const form = document.querySelector('form');
		if (!form || form.offsetParent === null) return '';
		const ids = [];
		for (const el of form.querySelectorAll('input,select,textarea')) {
			const t = (el.getAttribute('type') || '').toLowerCase();
			if (t === 'hidden') continue;
			if (el.offsetParent === null) continue;
			const key = (el.getAttribute('name') || el.id || el.getAttribute('aria-label') || '').trim().toLowerCase();
			if (key) ids.push(key);
		}
		if (ids.length === 0) return '';
		return ids.join('|');
	}`)
	if err != nil || res == nil {
		return ""
	}
	return res.Value.String()
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
		return nil
	}
	return checkATSBlockedState(txt.Value.String())
}

func (f *greenhouseRodFiller) NavigateNext(_ context.Context) (bool, error) {
	// Anchor the step we are leaving BEFORE the click.
	// If the current form step is not identifiable (no visible controls), we cannot
	// know what we are advancing FROM — leave the transition unconfirmed so that
	// no mappings are written based on ambiguous provenance.
	prevMarker := greenhouseStepMarker(f.page)
	if prevMarker == "" {
		return false, fmt.Errorf("external_ats: cannot identify current form step — leaving transition unconfirmed")
	}
	prevURL := currentPageURL(f.page)

	res, err := f.page.Eval(`() => {
		const btn = document.querySelector(
			'button[type="submit"], input[type="submit"], button.btn-submit, #submit_app'
		);
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
		time.Sleep(200 * time.Millisecond)

		newMarker := greenhouseStepMarker(f.page)
		if newMarker == "" || newMarker == prevMarker {
			// Same step still showing, or form not yet readable — keep waiting.
			continue
		}

		newURL := currentPageURL(f.page)
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

func (f *greenhouseRodFiller) IsSubmitReady(_ context.Context) (bool, error) {
	res, err := f.page.Eval(`() => {
		const btn = document.querySelector(
			'button[type="submit"]#submit_app, input[type="submit"]#submit_app, button.btn-primary[type="submit"]'
		);
		return !!(btn && !btn.disabled);
	}`)
	if err != nil {
		return false, nil
	}
	return res.Value.Bool(), nil
}

func (f *greenhouseRodFiller) ClickSubmit(_ context.Context) error {
	res, err := f.page.Eval(`() => {
		const btn = document.querySelector(
			'button[type="submit"]#submit_app, input[type="submit"]#submit_app, button.btn-primary[type="submit"]'
		);
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
