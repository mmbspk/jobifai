package bot

// adaptive_runner.go — wires the adaptive apply engine into LinkedIn Easy Apply
// and SEEK Quick Apply entry points (story #133).
//
// Design:
//   - adaptiveEasyApply replaces legacy easyApply when engine=adaptive.
//     It reuses existing pre-flight checks (identity, unresolved-submission)
//     and CTA-click logic, then drives each form step through the
//     snapshot → StepInterpreter → StepExecutor pipeline.
//   - shadowEasyApply asserts that shadow-mode code CANNOT issue browser actions.
//     Shadow mode runs the legacy path unchanged; this stub errors if called.
//   - linkedInRodFiller implements FieldFiller using the existing Rod-based JS
//     helpers (jsFillByQuestionAnswer, jsClickApplyNavigation).
//
// Guard: neither function may be called with engine=shadow; routing in manager.go
// enforces this — these stubs fire if routing is misconfigured.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/rs/zerolog/log"
	"github.com/user/jobifai/internal/domain"
)

// errShadowBrowserAction is returned when shadow-mode code attempts a browser
// action. It signals a programming error — shadow must never replace the legacy
// fill path; this is the safety net.
var errShadowBrowserAction = errors.New("adaptive_runner: shadow mode must not issue browser actions (bug: wrong routing)")

// Confirmation observation never retries Submit. A timeout leaves the durable
// submit intent for reconciliation instead of reporting an application as sent.
func waitAdaptiveConfirmation(ctx context.Context, confirmed func() bool) bool {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return false
		}
		if confirmed() {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
}

// resolvedFilledRefs returns only the FieldResolutions whose ref appears in filledRefs.
// This restricts cache writes to fields the executor actually filled, avoiding entries
// for fields that were interpreted but could not be written (e.g. DOM element missing).
func resolvedFilledRefs(fields []FieldResolution, filledRefs []string) []FieldResolution {
	if len(filledRefs) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(filledRefs))
	for _, ref := range filledRefs {
		set[ref] = struct{}{}
	}
	out := make([]FieldResolution, 0, len(filledRefs))
	for _, r := range fields {
		if _, ok := set[r.Ref]; ok {
			out = append(out, r)
		}
	}
	return out
}

// shadowEasyApply asserts the shadow constraint: if this function is ever called
// it means the routing incorrectly sent shadow to the adaptive path.
func shadowEasyApply() error {
	return errShadowBrowserAction
}

// shadowSeekApply is the SEEK equivalent of shadowEasyApply.
func shadowSeekApply() error {
	return errShadowBrowserAction
}

// adaptiveEasyApply drives LinkedIn Easy Apply through the adaptive pipeline.
// It runs the same pre-flight checks as the legacy path and then uses
// StepInterpreter + StepExecutor for each modal step.
func (b *Bot) adaptiveEasyApply(ctx context.Context, page *rod.Page, lazy *lazyDocGen, ac *AttemptContext) (retErr error) {
	ctx, cancel := beginAdaptiveRun(ctx)
	defer cancel()
	if ac != nil && ac.Engine == domain.ApplyEngineShadow {
		return shadowEasyApply()
	}

	// Pre-flight checks identical to legacy easyApply.
	if lazy == nil {
		return fmt.Errorf("%w: missing declared job", errLinkedInIdentity)
	}
	if b.cfg.DB == nil || ac == nil || ac.AttemptID == "" {
		return ErrNoActiveAttempt
	}
	if page == nil {
		return fmt.Errorf("%w: application page is missing", errLinkedInIdentity)
	}
	page, lazy.ctx = page.Context(ctx), ctx
	if err := unresolvedSubmission(b.cfg.DB, b.cfg.UserID, lazy.job.ID, lazy.job.URL, lazy.attemptID); err != nil {
		return fmt.Errorf("%w: %v", errLinkedInSubmissionUnknown, err)
	}

	submitClicked := false
	defer func() {
		if submitClicked && retErr != nil {
			retErr = fmt.Errorf("%w: %v", errLinkedInSubmissionUnknown, retErr)
		}
	}()

	// Wait for page load.
	if ready, err := page.Eval(`() => document.readyState`); err != nil || ready.Value.String() != "complete" {
		if err := page.Timeout(30 * time.Second).WaitLoad(); err != nil {
			return fmt.Errorf("adaptive: wait load: %w", err)
		}
	}

	// Click the Easy Apply CTA (same as legacy clickCTA path).
	expectedID := linkedInExpectedID(lazy.job.URL)
	r, err := resolveLinkedInJob(page, expectedID, true, lazy.job)
	logLinkedInResolution(r, lazy.job, lazy.attemptID, "adaptive_cta_click")
	if err != nil {
		return err
	}
	if err := r.resolutionError(); err != nil {
		return err
	}
	if r.Kind == "already_applied" {
		return errAlreadyApplied
	}

	b.log.Info().Str("attempt_id", lazy.attemptID).Str("job_id", lazy.job.ID).Msg("adaptive: CTA clicked, waiting for modal")

	// Wait for the modal to render (up to 20s): scan until at least one field appears.
	var firstFields []formField
	var firstPage *rod.Page
	initialStepReady := false
	modalDeadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(modalDeadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		firstFields, firstPage, _ = b.scanAdaptiveStep(ctx, page)
		if len(firstFields) > 0 {
			initialStepReady = true
			break
		}
		if _, kind, found, _, err := b.peekEasyApplyPrimary(page); err == nil && found && applyPrimaryIsSubmit(kind) {
			initialStepReady = true
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !initialStepReady {
		return fmt.Errorf("adaptive: modal did not render fields within timeout")
	}
	activePage := page
	if firstPage != nil {
		activePage = firstPage
	}

	b.log.Info().Str("attempt_id", lazy.attemptID).Int("initial_fields", len(firstFields)).Msg("adaptive: modal ready, entering step loop")
	employerScope := nativeEmployerScope(ctx, page, "linkedin", lazy.job)

	// Set up the adaptive pipeline components.
	profile := b.currentProfile()
	si := &StepInterpreter{
		LLM:     boundFormLLM(b.cfg.AdaptiveLLM),
		Mapping: b.cfg.AdaptiveMapping,
	}
	guard := &SubmitGuard{
		DB:        b.cfg.DB,
		AttemptID: lazy.attemptID,
		UserID:    b.cfg.UserID,
	}
	se := NewStepExecutor(DefaultStepExecBounds(), guard)
	se.BeforeSideEffect = func(ctx context.Context) error {
		if err := b.checkAdaptiveSideEffect(); err != nil {
			return err
		}
		if err := killSwitchCheck(b.cfg.EngineStore, ac); err != nil {
			return err
		}
		r, err := resolveLinkedInJob(page.Context(ctx), linkedInExpectedID(lazy.job.URL), false, lazy.job)
		if err != nil {
			return err
		}
		if !r.IdentityVerified {
			return r.resolutionError()
		}
		return nil
	}
	filler := &linkedInRodFiller{page: activePage, bot: b, lazy: lazy}

	const maxModalSteps = 30
	for stepIdx := 0; stepIdx < maxModalSteps; stepIdx++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Scan fields for this step.
		var fields []formField
		if stepIdx == 0 {
			fields = firstFields // already scanned above
		} else {
			var scanPage *rod.Page
			fields, scanPage, _ = b.scanAdaptiveStep(ctx, page)
			if scanPage != nil {
				filler.page = scanPage
			}
		}

		// Extract step title from the modal heading.
		stepTitle := fmt.Sprintf("step_%d", stepIdx)
		if ti, terr := page.Eval(`() => { const h = document.querySelector('[role="dialog"] h3, [role="dialog"] h2, [role="dialog"] h1, .jobs-easy-apply-modal h3'); return h ? h.textContent.trim() : '' }`); terr == nil && ti.Value.String() != "" {
			stepTitle = ti.Value.String()
		}

		snap := captureStepSnapshot(fields, nil, "linkedin", stepIdx, stepTitle)
		scopeJobDependentFields(&snap, lazy.job)
		lazy.applyCaps(capsFromFormFields(fields))
		snap.EmployerKey = employerScope

		// Interpret: deterministic → mapping → LLM.
		interp, err := si.InterpretStep(ctx, snap, profile)
		if err != nil {
			return fmt.Errorf("adaptive: interpret step %d: %w", stepIdx, err)
		}

		if interp.Paused {
			recordAdaptiveProposals(ctx, b.cfg.AdaptiveMapping, snap, interp)
			log.Warn().Int("step", stepIdx).Str("reason", string(interp.PauseReason)).Msg("adaptive: step paused, needs human review")
			return fmt.Errorf("%w: step %d (%s)", errAdaptiveQuestionReview, stepIdx, interp.PauseReason)
		}

		// Execute: fill fields + navigate.
		recordAdaptiveProposals(ctx, b.cfg.AdaptiveMapping, snap, interp)
		result, err := se.ExecuteStep(ctx, snap, interp, filler)
		invalidateFailedMappings(ctx, b.cfg.AdaptiveMapping, snap, interp, result)
		if err != nil && result.Outcome != StepOutcomeSubmitted {
			return fmt.Errorf("adaptive: execute step %d: %w", stepIdx, err)
		}

		// Persist verified resolutions only on confirmed transitions.
		// For submission, require a positive LinkedIn confirmation signal — nil
		// click error alone is not sufficient evidence the application was sent.
		var submissionConfirmed bool
		if result.Outcome == StepOutcomeSubmitted && result.Err == nil {
			expectedID := linkedInExpectedID(lazy.job.URL)
			submissionConfirmed = waitAdaptiveConfirmation(ctx, func() bool {
				return linkedInSubmissionConfirmed(filler.page, expectedID, lazy.job.Company)
			})
			if !submissionConfirmed {
				b.log.Warn().Str("attempt_id", lazy.attemptID).Int("step", stepIdx).
					Msg("adaptive: submit clicked but LinkedIn confirmation signal absent — skipping mapping write")
			}
		}
		writeConfirmedStep(ctx, b.cfg.AdaptiveMapping, snap, interp, result, submissionConfirmed)

		switch result.Outcome {
		case StepOutcomeSubmitted:
			submitClicked = true
			if !submissionConfirmed {
				return errLinkedInSubmissionUnknown
			}
			b.log.Info().Str("attempt_id", lazy.attemptID).Int("step", stepIdx).Msg("adaptive: submit clicked")
			return nil
		case StepOutcomeAdvanced:
			b.log.Debug().Int("step", stepIdx).Int("filled", result.FieldsFilled).Msg("adaptive: step advanced")
		case StepOutcomePaused:
			return fmt.Errorf("adaptive: step %d paused", stepIdx)
		default:
			return fmt.Errorf("adaptive: step %d unexpected outcome: %s", stepIdx, result.Outcome)
		}
	}

	return fmt.Errorf("adaptive: exceeded max modal steps (%d)", maxModalSteps)
}

// scanAdaptiveStep runs jsScanFields on the page (and iframes) and returns
// the fields plus the page that hosted the form. Pure read-only.
func (b *Bot) scanAdaptiveStep(ctx context.Context, page *rod.Page) ([]formField, *rod.Page, error) {
	pages := []*rod.Page{page}
	if frames, err := page.Elements("iframe, frame"); err == nil {
		for _, fr := range frames {
			if fp, ferr := fr.Frame(); ferr == nil && fp != nil {
				pages = append(pages, fp)
			}
		}
	}
	for _, p := range pages {
		res, err := p.Context(ctx).Timeout(10*time.Second).Eval(jsScanFields, map[string]interface{}{"includeAnsweredRadios": true, "includeAnsweredFields": true})
		if err != nil {
			continue
		}
		var fields []formField
		if err := json.Unmarshal([]byte(res.Value.String()), &fields); err != nil {
			continue
		}
		if len(fields) > 0 {
			return fields, p, nil
		}
	}
	return nil, nil, nil
}

// ── step-advancement helpers ──────────────────────────────────────────────────

// linkedInStepProgress reads the aria-valuenow attribute from the LinkedIn Easy
// Apply progress bar inside the modal dialog. Returns -1 when the bar cannot be
// read; a non-negative value is the current step index (1-based).
//
// This is the primary advancement signal for LinkedIn. A missing or unreadable
// progress bar is never treated as evidence of advancement — the caller must
// fall back to the final-review-page check in that case.
func linkedInStepProgress(page *rod.Page) int { return adaptiveStepProgress(page) }
func seekStepProgress(page *rod.Page) int     { return adaptiveStepProgress(page) }

func adaptiveStepProgress(page *rod.Page) int {
	result, err := page.Eval(`() => {` + jsLinkedInApplyContainerFn + `
  const scope=resolveApplyContainer();if(!scope)return -1;
  const bars=allInDOM(scope,'progress,[role="progressbar"]').filter(el=>{const r=el.getBoundingClientRect();if(!r.width||!r.height)return false;for(let p=el;p;p=p.parentElement){const s=getComputedStyle(p);if(p.hidden||p.getAttribute('aria-hidden')==='true'||s.display==='none'||s.visibility==='hidden')return false;}return true;});
  if(bars.length!==1)return -1;
  const value=bars[0].getAttribute('aria-valuenow')||bars[0].getAttribute('value');
  if(value===null||value.trim()==='')return -1;
  const n=Number(value);return Number.isFinite(n)&&n>=0?n:-1;
 }`)
	if err != nil || result == nil {
		return -1
	}
	return result.Value.Int()
}

// writeConfirmedStep persists field mappings to the mapping store only when
// the step outcome is confirmed:
//   - StepOutcomeAdvanced: NavigateNext returned (true, nil), proving the form
//     actually moved to a different step before this is called.
//   - StepOutcomeSubmitted: submissionConfirmed must be true, meaning the caller
//     has verified a platform-specific positive confirmation signal (not just nil
//     click error). Pass false to skip the write.
//
// All other outcomes (StepOutcomeError, StepOutcomePaused, etc.) are silently
// ignored — no mappings are written.
func writeConfirmedStep(ctx context.Context, store MappingReadWriter, snap StepSnapshot, interp StepInterpretation, result StepExecResult, submissionConfirmed bool) {
	if store == nil {
		return
	}
	fields := resolvedFilledRefs(interp.Fields, result.FilledRefs)
	if len(fields) == 0 || result.Err != nil {
		return
	}
	switch result.Outcome {
	case StepOutcomeAdvanced:
		_ = store.Write(ctx, snap, fields)
	case StepOutcomeSubmitted:
		if submissionConfirmed {
			_ = store.Write(ctx, snap, fields)
		}
	}
}

// ── linkedInRodFiller ─────────────────────────────────────────────────────────

// linkedInRodFiller implements FieldFiller for LinkedIn Easy Apply using the
// existing Rod-based JS helpers (jsFillByQuestionAnswer, jsClickApplyNavigation).
type linkedInRodFiller struct {
	page *rod.Page
	bot  *Bot
	lazy *lazyDocGen
}

func (f *linkedInRodFiller) Fill(ctx context.Context, field SnapField, res FieldResolution) FieldFillResult {
	if res.Action == ActionSkip {
		return FieldFillResult{Skipped: true}
	}
	if res.Action == ActionUpload {
		return f.bot.fillAdaptiveDocument(ctx, f.page, f.lazy, field)
	}

	// ActionFill: use jsFillByQuestionAnswer for text/radio/select/textarea.
	return fillAdaptiveControl(ctx, f.page, field, res.Value)
}

func (f *linkedInRodFiller) NavigateNext(ctx context.Context) (bool, error) {
	page := f.page.Context(ctx)
	// Capture pre-click progress bar step index as the advancement baseline.
	prevProgress := linkedInStepProgress(page)
	_, ok, err := f.bot.clickEasyApplyNavigation(page)
	if err != nil || !ok {
		return ok, err
	}
	// Poll up to 2 s for positive evidence of a different active step.
	// Two independent signals are accepted:
	//   1. linkedInPrepareAtFinalReview — unambiguous transition to the review page.
	//   2. Progress bar aria-valuenow changed — confirmed intermediate step advance.
	// A missing or unreadable progress bar never counts as evidence.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if f.bot.linkedInPrepareAtFinalReview(page) {
			return true, nil
		}
		if newProgress := linkedInStepProgress(page); newProgress >= 0 && prevProgress >= 0 && newProgress != prevProgress {
			return true, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false, fmt.Errorf("adaptive: step did not advance — no progress-bar or review-page signal within 2s (prev=%d)", prevProgress)
}

func (f *linkedInRodFiller) IsSubmitReady(ctx context.Context) (bool, error) {
	page := f.page.Context(ctx)
	_, kind, found, _, err := f.bot.peekEasyApplyPrimary(page)
	if err != nil || !found {
		return false, nil
	}
	ready := applyPrimaryIsSubmit(kind)
	if ready && f.lazy != nil {
		if f.lazy.policyBlocked() {
			return false, fmt.Errorf("documents: %s", f.lazy.holdReason)
		}
		if resumePath, _ := f.lazy.peek(); resumePath != "" {
			if err := f.bot.linkedInVerifyFinalReviewResume(page, resumePath); err != nil {
				return false, err
			}
		}
	}
	return ready, nil
}

func (f *linkedInRodFiller) ClickSubmit(ctx context.Context) error {
	page := f.page.Context(ctx)
	ready, err := page.Eval(jsLinkedInAdaptiveSubmit, false)
	if err != nil {
		return err
	}
	if ready == nil || !ready.Value.Bool() {
		return errors.New("adaptive: submit control is missing or ambiguous")
	}
	button, err := page.ElementByJS(rod.Eval(jsLinkedInAdaptiveSubmit, true))
	if err != nil {
		return err
	}
	return button.Click(proto.InputMouseButtonLeft, 1)
}

const jsLinkedInAdaptiveSubmit = `(element)=>{` + jsLinkedInApplyContainerFn + `
 const scope=resolveApplyContainer();if(!scope)return element?null:false;
 const candidates=allInDOM(scope,'button,[role=button]').filter(el=>{
  const label=(el.getAttribute('aria-label')||el.textContent||'').trim().toLowerCase();
  if(!/^(submit|submit application|send application)$/.test(label))return false;
  if(el.disabled||el.getAttribute('aria-disabled')==='true')return false;
  const r=el.getBoundingClientRect();if(!r.width||!r.height)return false;
  for(let p=el;p;p=p.parentElement){const s=getComputedStyle(p);if(p.hidden||p.inert||p.getAttribute('aria-hidden')==='true'||s.display==='none'||s.visibility==='hidden')return false;}
  return true;
 });
 return element?(candidates.length===1?candidates[0]:null):candidates.length===1;
}`

// refIndex extracts the numeric index from a SnapField ref of the form "f{n}".
func refIndex(ref string) (int, error) {
	if !strings.HasPrefix(ref, "f") {
		return 0, fmt.Errorf("adaptive: unexpected ref format %q", ref)
	}
	n, err := strconv.Atoi(ref[1:])
	if err != nil {
		return 0, fmt.Errorf("adaptive: cannot parse ref index from %q: %w", ref, err)
	}
	if n < 0 {
		return 0, fmt.Errorf("adaptive: negative ref index in %q", ref)
	}
	return n, nil
}

// adaptiveSeekApply drives the SEEK Quick Apply form through the adaptive pipeline.
// It runs the same session pre-flight checks as the legacy seekApply but replaces
// fillFormStep with the snapshot → StepInterpreter → StepExecutor pipeline.
//
// Design note: SEEK's Quick Apply is a React SPA that renders one form section
// at a time. The adaptive runner scans each visible section, interprets the
// fields, executes them, then clicks Continue/Submit. The session lifecycle
// (login, session persistence, auth-wall detection) mirrors the legacy path
// to ensure the same reliability guarantees.
func (b *Bot) adaptiveSeekApply(ctx context.Context, page *rod.Page, lazy *lazyDocGen, ac *AttemptContext) (retErr error) {
	ctx, cancel := beginAdaptiveRun(ctx)
	defer cancel()
	if ac != nil && ac.Engine == domain.ApplyEngineShadow {
		return shadowSeekApply()
	}
	if lazy == nil {
		return fmt.Errorf("seek adaptive: missing declared job: %w", errLinkedInIdentity)
	}
	if b.cfg.DB == nil || ac == nil || ac.AttemptID == "" {
		return ErrNoActiveAttempt
	}
	if page == nil {
		return fmt.Errorf("%w: application page is missing", errLinkedInIdentity)
	}
	page, lazy.ctx = page.Context(ctx), ctx
	if err := unresolvedSubmission(b.cfg.DB, b.cfg.UserID, lazy.job.ID, lazy.job.URL, lazy.attemptID); err != nil {
		return fmt.Errorf("%w: %v", errSeekSubmitUnconfirmed, err)
	}

	// Wait for the job page to be fully loaded.
	if ready, err := page.Eval(`() => document.readyState`); err != nil || ready.Value.String() != "complete" {
		if err := page.Timeout(30 * time.Second).WaitLoad(); err != nil {
			return fmt.Errorf("seek adaptive: wait load: %w", err)
		}
	}
	_ = page.Timeout(5 * time.Second).WaitStable(2 * time.Second)
	if err := verifyAdaptiveSeekIdentity(ctx, page, lazy, true); err != nil {
		return err
	}
	employerScope := nativeEmployerScope(ctx, page, "seek", lazy.job)

	// Persist session tokens before any navigation so a fresh rotate is recorded.
	b.seekPersistSession(page)

	// Verify the session is authenticated.
	if err := b.seekEnsureLoggedIn(page); err != nil {
		return fmt.Errorf("seek adaptive: session expired — re-add Seek credentials: %w", err)
	}
	if err := verifyAdaptiveSeekIdentity(ctx, page, lazy, false); err != nil {
		return err
	}
	if b.cfg.EnableExternalATS {
		destination, err := seekExternalHandoff(ctx, page)
		if err != nil {
			return err
		}
		if classifyExternalATS(destination) != ATSKindUnknown {
			return b.externalATSApply(ctx, page, lazy, ac, destination)
		}
	}

	// Open Quick Apply form.
	if err := b.seekOpenQuickApplyForm(page, b.policiesRequireVerifiedDocuments()); err != nil {
		return fmt.Errorf("seek adaptive: open form: %w", err)
	}
	b.humanPause()

	// Wait past any auth wall that may appear after the CTA click.
	if onLogin, err := b.seekWaitPastLogin(page); err != nil {
		return fmt.Errorf("seek adaptive: wait form load: %w", err)
	} else if onLogin {
		if recoverErr := b.seekEnsureLoggedIn(page); recoverErr != nil {
			return fmt.Errorf("seek adaptive: session expired after CTA click: %w", recoverErr)
		}
	}

	// Fast-path: SEEK sometimes silently pre-submits (pre-filled profile).
	if b.seekIsPostApplySuccess(page) {
		if b.cfg.Documents != nil && b.policiesRequireVerifiedDocuments() {
			return fmt.Errorf("seek adaptive: Quick Apply submitted before document scan — apply manually on Seek")
		}
		b.log.Info().Msg("seek adaptive: possible silent success page — verifying on job page")
		jobURL := lazy.job.URL
		if err := b.seekVerifyAppliedOnJobPage(page, jobURL); err != nil {
			return fmt.Errorf("seek adaptive: success signal without job-page confirmation: %w", err)
		}
		b.log.Info().Msg("seek adaptive: pre-submitted successfully ✓")
		return nil
	}

	// Ensure the form panel is visible.
	if err := b.ensureSeekApplyFormVisible(page, lazy.job.URL); err != nil {
		return fmt.Errorf("seek adaptive: ensure form visible: %w", err)
	}

	lazy.applyCaps(probeApplyFormCaps(page))
	lazy.ensureMaterialized()
	if lazy.policyBlocked() {
		return fmt.Errorf("seek adaptive: documents: %s", lazy.holdReason)
	}

	b.log.Info().Str("attempt_id", lazy.attemptID).Str("job_id", lazy.job.ID).Msg("seek adaptive: form open, entering step loop")

	profile := b.currentProfile()
	si := &StepInterpreter{
		LLM:     boundFormLLM(b.cfg.AdaptiveLLM),
		Mapping: b.cfg.AdaptiveMapping,
	}
	guard := &SubmitGuard{
		DB:        b.cfg.DB,
		AttemptID: lazy.attemptID,
		UserID:    b.cfg.UserID,
	}
	se := NewStepExecutor(DefaultStepExecBounds(), guard)
	se.BeforeSideEffect = func(ctx context.Context) error {
		if err := b.checkAdaptiveSideEffect(); err != nil {
			return err
		}
		if err := killSwitchCheck(b.cfg.EngineStore, ac); err != nil {
			return err
		}
		return verifyAdaptiveSeekIdentity(ctx, page, lazy, false)
	}
	filler := &seekRodFiller{page: page, bot: b, lazy: lazy}

	const maxFormSteps = 15
	for stepIdx := 0; stepIdx < maxFormSteps; stepIdx++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if b.cfg.BrowserMeter != nil {
			if !DestructiveActionAllowed(b.cfg.BrowserMeter) {
				return fmt.Errorf("seek adaptive: %w", b.cfg.BrowserMeter.Err())
			}
		}

		// Scroll before scanning so below-the-fold fields are in view.
		_, _ = page.Eval(`() => { window.scrollTo(0, document.body.scrollHeight); }`)
		time.Sleep(300 * time.Millisecond)

		fields, scanPage, _ := b.scanAdaptiveStep(ctx, page)
		if scanPage != nil && scanPage != page {
			filler.page = scanPage
		}

		stepTitle := fmt.Sprintf("seek_step_%d", stepIdx)
		if ti, terr := page.Eval(`() => { const h = document.querySelector('h1,h2,h3,[data-testid*="heading"]'); return h ? h.textContent.trim() : '' }`); terr == nil && ti.Value.String() != "" {
			stepTitle = ti.Value.String()
		}

		snap := captureStepSnapshot(fields, nil, "seek", stepIdx, stepTitle)
		scopeJobDependentFields(&snap, lazy.job)
		lazy.applyCaps(capsFromFormFields(fields))
		snap.EmployerKey = employerScope

		interp, err := si.InterpretStep(ctx, snap, profile)
		if err != nil {
			return fmt.Errorf("seek adaptive: interpret step %d: %w", stepIdx, err)
		}
		if interp.Paused {
			recordAdaptiveProposals(ctx, b.cfg.AdaptiveMapping, snap, interp)
			return fmt.Errorf("%w: step %d (%s)", errAdaptiveQuestionReview, stepIdx, interp.PauseReason)
		}

		recordAdaptiveProposals(ctx, b.cfg.AdaptiveMapping, snap, interp)
		result, err := se.ExecuteStep(ctx, snap, interp, filler)
		invalidateFailedMappings(ctx, b.cfg.AdaptiveMapping, snap, interp, result)
		if err != nil && result.Outcome != StepOutcomeSubmitted {
			return fmt.Errorf("seek adaptive: execute step %d: %w", stepIdx, err)
		}

		// Persist verified resolutions only on confirmed transitions.
		// For submission, require a positive SEEK post-apply success signal — nil
		// click error alone is not sufficient evidence the application was sent.
		var submissionConfirmed bool
		if result.Outcome == StepOutcomeSubmitted && result.Err == nil {
			submissionConfirmed = b.seekWaitSubmitConfirmation(page.Context(ctx), lazy.job.ID, lazy.job.URL) == nil
			if !submissionConfirmed {
				b.log.Warn().Str("attempt_id", lazy.attemptID).Int("step", stepIdx).
					Msg("seek adaptive: submit clicked but no post-apply success signal — skipping mapping write")
			}
		}
		writeConfirmedStep(ctx, b.cfg.AdaptiveMapping, snap, interp, result, submissionConfirmed)

		switch result.Outcome {
		case StepOutcomeSubmitted:
			if !submissionConfirmed {
				return errSeekSubmitUnconfirmed
			}
			b.seekPersistSession(page)
			b.log.Info().Str("attempt_id", lazy.attemptID).Int("step", stepIdx).Msg("seek adaptive: submit clicked")
			return nil
		case StepOutcomeAdvanced:
			b.log.Debug().Int("step", stepIdx).Int("filled", result.FieldsFilled).Msg("seek adaptive: step advanced")
		case StepOutcomePaused:
			return fmt.Errorf("seek adaptive: step %d paused", stepIdx)
		default:
			return fmt.Errorf("seek adaptive: step %d unexpected outcome: %s", stepIdx, result.Outcome)
		}
	}

	return fmt.Errorf("seek adaptive: exceeded max form steps (%d)", maxFormSteps)
}

// ── seekRodFiller ─────────────────────────────────────────────────────────────

// seekRodFiller implements FieldFiller for SEEK Quick Apply forms.
// Fill uses the same jsFillByQuestionAnswer helper as the LinkedIn filler.
// Navigation uses seekFindActionButton: Continue for intermediate steps, Submit
// for the final review step.
type seekRodFiller struct {
	page *rod.Page
	bot  *Bot
	lazy *lazyDocGen
}

func (f *seekRodFiller) Fill(ctx context.Context, field SnapField, res FieldResolution) FieldFillResult {
	if res.Action == ActionSkip {
		return FieldFillResult{Skipped: true}
	}
	if res.Action == ActionUpload {
		return f.bot.fillAdaptiveDocument(ctx, f.page, f.lazy, field)
	}
	return fillAdaptiveControl(ctx, f.page, field, res.Value)
}

func (f *seekRodFiller) NavigateNext(ctx context.Context) (bool, error) {
	page := f.page.Context(ctx)
	btn, isSubmit, err := seekAdaptiveActionButton(page)
	if err != nil {
		return false, err
	}
	if btn == nil || isSubmit {
		return false, nil
	}
	// Capture pre-click progress bar step index as the advancement baseline.
	prevProgress := seekStepProgress(page)
	if err := btn.Click(proto.InputMouseButtonLeft, 1); err != nil {
		return false, fmt.Errorf("seek adaptive: click continue: %w", err)
	}
	// Poll up to 2 s for positive evidence of a different active step.
	// Three signals are accepted (checked in priority order):
	//   1. Validation errors — explicit rejection; return false immediately.
	//   2. Progress bar aria-valuenow changed — confirmed intermediate step advance.
	//   3. Submit button appeared where Continue was — confirmed final-step transition.
	// A missing or unreadable progress bar never counts as evidence.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		hasErrors, _ := page.Eval(`() =>
			document.querySelectorAll('[aria-invalid="true"],[data-testid*="error"],[class*="error-message"]').length > 0
		`)
		if hasErrors != nil && hasErrors.Value.Bool() {
			return false, fmt.Errorf("seek adaptive: continue rejected — validation errors visible after click")
		}
		if newProgress := seekStepProgress(page); newProgress >= 0 && prevProgress >= 0 && newProgress != prevProgress {
			return true, nil
		}
		if _, nowSubmit, err := seekAdaptiveActionButton(page); err == nil && nowSubmit {
			return true, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false, fmt.Errorf("seek adaptive: step did not advance — no progress-bar or submit-button signal within 2s (prev=%d)", prevProgress)
}

func (f *seekRodFiller) IsSubmitReady(ctx context.Context) (bool, error) {
	page := f.page.Context(ctx)
	_, isSubmit, err := seekAdaptiveActionButton(page)
	return isSubmit, err
}

func (f *seekRodFiller) ClickSubmit(ctx context.Context) error {
	page := f.page.Context(ctx)
	btn, isSubmit, err := seekAdaptiveActionButton(page)
	if err != nil {
		return err
	}
	if btn == nil {
		return fmt.Errorf("seek adaptive: submit button not found")
	}
	if !isSubmit {
		return fmt.Errorf("seek adaptive: expected submit button but found continue")
	}
	if err := btn.Click(proto.InputMouseButtonLeft, 1); err != nil {
		return fmt.Errorf("seek adaptive: click submit: %w", err)
	}
	return nil
}

const jsSeekAdaptiveAction = `(element)=>{` + jsLinkedInApplyContainerFn + `
 const scope=resolveApplyContainer();if(!scope)return element?null:'';
 const actionable=el=>{const r=el.getBoundingClientRect();if(!r.width||!r.height||el.disabled||el.getAttribute('aria-disabled')==='true')return false;for(let p=el;p;p=p.parentElement){const s=getComputedStyle(p);if(p.hidden||p.inert||p.getAttribute('aria-hidden')==='true'||s.display==='none'||s.visibility==='hidden')return false;}return true;};
 const buttons=allInDOM(scope,'button,input[type=submit],[role=button]').filter(actionable);
 const label=el=>(el.getAttribute('aria-label')||el.textContent||el.value||'').trim().toLowerCase();
 const next=buttons.filter(el=>/^(continue|next|review|review application|save and continue)$/.test(label(el)));
 const submit=buttons.filter(el=>/^(submit|submit application|send application)$/.test(label(el)));
 const choices=next.length?next:submit;
 if(element)return choices.length===1?choices[0]:null;
 return choices.length>1?'ambiguous':next.length?'next':submit.length?'submit':'';
}`

func seekAdaptiveActionButton(page *rod.Page) (*rod.Element, bool, error) {
	result, err := page.Eval(jsSeekAdaptiveAction, false)
	if err != nil {
		return nil, false, err
	}
	kind := result.Value.String()
	if kind == "ambiguous" {
		return nil, false, fmt.Errorf("%w: multiple current navigation controls", errAdaptiveQuestionReview)
	}
	if kind == "" {
		return nil, false, nil
	}
	element, err := page.ElementByJS(rod.Eval(jsSeekAdaptiveAction, true))
	return element, kind == "submit", err
}
