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
	"github.com/rs/zerolog/log"
	"github.com/user/jobifai/internal/domain"
)

// errShadowBrowserAction is returned when shadow-mode code attempts a browser
// action. It signals a programming error — shadow must never replace the legacy
// fill path; this is the safety net.
var errShadowBrowserAction = errors.New("adaptive_runner: shadow mode must not issue browser actions (bug: wrong routing)")
var errAdaptiveStepUnchanged = errors.New("adaptive: step did not advance")

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
	filler := &linkedInRodFiller{page: activePage, bot: b, lazy: lazy, beforeClick: se.BeforeSideEffect}
	defer func() {
		if retErr != nil {
			if dir := saveAdaptiveDiagnostic(filler.page, "linkedin"); dir != "" {
				b.log.Warn().Str("diagnostic_dir", dir).Msg("private application diagnostic saved")
			}
		}
	}()

	const maxModalSteps = 30
	rescannedSteps := map[int]int{}
	var retryFields []formField
	for stepIdx := 0; stepIdx < maxModalSteps; stepIdx++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Scan fields for this step.
		var fields []formField
		if retryFields != nil {
			fields = retryFields
			retryFields = nil
		} else if stepIdx == 0 {
			fields = firstFields // already scanned above
		} else {
			var scanPage *rod.Page
			var scanErr error
			fields, scanPage, scanErr = b.scanAdaptiveStep(ctx, page)
			if scanErr != nil {
				return fmt.Errorf("adaptive: scan step %d: %w", stepIdx, scanErr)
			}
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
		selects := 0
		for _, field := range fields {
			if field.Type == "select" {
				selects++
			}
		}
		b.log.Debug().Int("step", stepIdx).Int("fields", len(fields)).Int("selects", selects).Msg("adaptive: scanned current controls")
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
		verifiedRefs := writeVerifiedFilledStep(ctx, b.cfg.AdaptiveMapping, snap, interp, result)
		invalidateFailedMappings(ctx, b.cfg.AdaptiveMapping, snap, interp, result)
		if errors.Is(err, errAdaptiveStepUnchanged) && rescannedSteps[stepIdx] < se.Bounds.MaxFillAttempts && !interpretationUploads(interp) {
			fresh, freshPage, changed, scanErr := b.newAdaptiveControls(ctx, page, snap, lazy.job)
			if scanErr != nil {
				return fmt.Errorf("adaptive: changed form could not be bound: %w", scanErr)
			}
			if scanErr == nil && freshPage != nil {
				progress := linkedInStepProgress(freshPage)
				if (filler.previousProgress >= 0 && progress > filler.previousProgress) || b.linkedInPrepareAtFinalReview(freshPage) {
					result.Outcome, result.Err, err = StepOutcomeAdvanced, nil, nil
					filler.page = freshPage
				} else if changed {
					retryFields, filler.page = fresh, freshPage
					rescannedSteps[stepIdx]++
					b.log.Debug().Int("step", stepIdx).Int("rescan", rescannedSteps[stepIdx]).Msg("adaptive: newly rendered controls require fresh interpretation")
					stepIdx--
					continue
				}
			}
		}
		if err != nil && result.Outcome != StepOutcomeSubmitted {
			return fmt.Errorf("adaptive: execute step %d: %w", stepIdx, err)
		}

		// Learn remaining trusted bindings only on confirmed transitions;
		// independently grounded fills were already learned above.
		// For submission, require a positive LinkedIn confirmation signal — nil
		// click error alone is not sufficient evidence the application was sent.
		var submissionConfirmed bool
		if result.Outcome == StepOutcomeSubmitted && result.Err == nil {
			expectedID := linkedInExpectedID(lazy.job.URL)
			submissionConfirmed = waitAdaptiveConfirmation(ctx, func() bool {
				return linkedInSubmissionConfirmed(filler.page, expectedID, lazy.job.Company, lazy.job.Title)
			})
			if !submissionConfirmed {
				b.log.Warn().Str("attempt_id", lazy.attemptID).Int("step", stepIdx).
					Msg("adaptive: submit clicked but LinkedIn confirmation signal absent — skipping mapping write")
			}
		}
		writeConfirmedRemainingStep(ctx, b.cfg.AdaptiveMapping, snap, interp, result, submissionConfirmed, verifiedRefs)

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
	var scanErr error
	if frames, err := page.Elements("iframe, frame"); err == nil {
		for _, fr := range frames {
			if fp, ferr := fr.Frame(); ferr == nil && fp != nil {
				pages = append(pages, fp)
			}
		}
	}
	for _, p := range pages {
		if _, err := p.Context(ctx).Timeout(2 * time.Second).Eval(jsLinkedInMaterializeForm); err != nil {
			return nil, nil, fmt.Errorf("materialize current application controls: %w", err)
		}
		res, err := p.Context(ctx).Timeout(10*time.Second).Eval(jsScanFields, map[string]interface{}{"includeAnsweredRadios": true, "includeAnsweredFields": true})
		if err != nil {
			if scanErr == nil {
				scanErr = fmt.Errorf("read current form controls: %w", err)
			}
			continue
		}
		var fields []formField
		if err := json.Unmarshal([]byte(res.Value.String()), &fields); err != nil {
			if scanErr == nil {
				scanErr = fmt.Errorf("decode current form controls: %w", err)
			}
			continue
		}
		if len(fields) > 0 {
			if err := b.prepareLinkedInScannedFields(ctx, p, fields); err != nil {
				return nil, nil, err
			}
			return fields, p, nil
		}
	}
	return nil, nil, scanErr
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
	page             *rod.Page
	bot              *Bot
	lazy             *lazyDocGen
	previousProgress int
	beforeClick      func(context.Context) error
}

func (f *linkedInRodFiller) Fill(ctx context.Context, field SnapField, res FieldResolution) FieldFillResult {
	if res.Action == ActionSkip {
		return FieldFillResult{Skipped: true}
	}
	if res.Action == ActionUpload {
		return f.bot.fillAdaptiveDocument(ctx, f.page, f.lazy, field)
	}

	var profile *domain.ResumeProfile
	if f.bot != nil {
		profile = f.bot.currentProfile()
	}
	return fillAdaptiveControl(ctx, f.page, field, res.Value, profile)
}

func (f *linkedInRodFiller) NavigateNext(ctx context.Context) (bool, error) {
	page := f.page.Context(ctx)
	// Capture pre-click progress bar step index as the advancement baseline.
	prevProgress := linkedInStepProgress(page)
	f.previousProgress = prevProgress
	_, ok, err := f.bot.clickEasyApplyNavigation(page)
	if err != nil || !ok {
		return ok, err
	}
	// Poll up to 2 s for positive evidence of a different active step.
	// Two independent signals are accepted:
	//   1. linkedInPrepareAtFinalReview — unambiguous transition to the review page.
	//   2. Progress bar aria-valuenow increased — confirmed intermediate step advance.
	// A missing or unreadable progress bar never counts as evidence.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if f.bot.linkedInPrepareAtFinalReview(page) {
			return true, nil
		}
		if newProgress := linkedInStepProgress(page); newProgress >= 0 && prevProgress >= 0 && newProgress > prevProgress {
			return true, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false, fmt.Errorf("%w — no progress-bar or review-page signal within 2s (prev=%d)", errAdaptiveStepUnchanged, prevProgress)
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
	return f.ClickSubmitWithIntent(ctx, nil)
}

func (f *linkedInRodFiller) ClickSubmitWithIntent(ctx context.Context, persistIntent func() error) error {
	return clickLinkedInAdaptiveSubmitWithIntent(ctx, f.page, func(ctx context.Context) error {
		if f.beforeClick != nil {
			if err := f.beforeClick(ctx); err != nil {
				return err
			}
		}
		if f.lazy != nil {
			r, err := resolveLinkedInJob(f.page.Context(ctx), linkedInExpectedID(f.lazy.job.URL), false, f.lazy.job)
			if err != nil {
				return err
			}
			if !r.IdentityVerified {
				return r.resolutionError()
			}
		}
		ready, err := f.IsSubmitReady(ctx)
		if err != nil {
			return err
		}
		if !ready {
			return errors.New("adaptive: submit control or approved document changed")
		}
		return nil
	}, persistIntent)
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
	filler := &seekRodFiller{page: page, bot: b, lazy: lazy, beforeClick: se.BeforeSideEffect}

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

		fields, scanPage, scanErr := b.scanAdaptiveStep(ctx, page)
		if scanErr != nil {
			return fmt.Errorf("seek adaptive: scan step %d: %w", stepIdx, scanErr)
		}
		if scanPage != nil && scanPage != page {
			filler.page = scanPage
		}
		if stepIdx == 0 && len(fields) == 0 && b.policiesRequireVerifiedDocuments() {
			diagnostic, diagnosticErr := page.Eval(jsSeekDocumentDiagnostic)
			if diagnosticErr == nil {
				b.log.Warn().Str("job_id", lazy.job.ID).RawJSON("document_shape", []byte(diagnostic.Value.String())).Msg("seek: document controls unresolved")
			}
			return fmt.Errorf("seek adaptive: document step has no resolved controls")
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
		verifiedRefs := writeVerifiedFilledStep(ctx, b.cfg.AdaptiveMapping, snap, interp, result)
		invalidateFailedMappings(ctx, b.cfg.AdaptiveMapping, snap, interp, result)
		if err != nil && result.Outcome != StepOutcomeSubmitted {
			return fmt.Errorf("seek adaptive: execute step %d: %w", stepIdx, err)
		}

		// Learn remaining trusted bindings only on confirmed transitions;
		// independently grounded fills were already learned above.
		// For submission, require a positive SEEK post-apply success signal — nil
		// click error alone is not sufficient evidence the application was sent.
		var submissionConfirmed bool
		if result.Outcome == StepOutcomeSubmitted && result.Err == nil {
			submissionConfirmed = b.seekWaitSubmitConfirmation(page.Context(ctx), lazy.job.ID, lazy.job.URL, lazy.job.Company) == nil
			if !submissionConfirmed {
				b.log.Warn().Str("attempt_id", lazy.attemptID).Int("step", stepIdx).
					Msg("seek adaptive: submit clicked but no post-apply success signal — skipping mapping write")
			}
		}
		writeConfirmedRemainingStep(ctx, b.cfg.AdaptiveMapping, snap, interp, result, submissionConfirmed, verifiedRefs)

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
	page        *rod.Page
	bot         *Bot
	lazy        *lazyDocGen
	beforeClick func(context.Context) error
}

func (f *seekRodFiller) Fill(ctx context.Context, field SnapField, res FieldResolution) FieldFillResult {
	if res.Action == ActionSkip {
		return FieldFillResult{Skipped: true}
	}
	if res.Action == ActionUpload {
		return f.bot.fillAdaptiveDocument(ctx, f.page, f.lazy, field)
	}
	var profile *domain.ResumeProfile
	if f.bot != nil {
		profile = f.bot.currentProfile()
	}
	return fillAdaptiveControl(ctx, f.page, field, res.Value, profile)
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
	prevHeading := seekApplicationHeading(page)
	if err := clickSeekAdaptiveAction(ctx, page, false, f.beforeClick); err != nil {
		return false, fmt.Errorf("seek adaptive: click continue: %w", err)
	}
	// Poll up to 2 s for positive evidence of a different active step.
	// Signals are checked in priority order:
	//   1. Validation errors — explicit rejection; return false immediately.
	//   2. Progress bar aria-valuenow increased — confirmed intermediate step advance.
	//   3. Scoped application heading changed — current full-page native layout.
	//   4. Submit button appeared where Continue was — confirmed final-step transition.
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
		if newProgress := seekStepProgress(page); newProgress >= 0 && prevProgress >= 0 && newProgress > prevProgress {
			return true, nil
		}
		if heading := seekApplicationHeading(page); prevHeading != "" && heading != "" && heading != prevHeading {
			return true, nil
		}
		if _, nowSubmit, err := seekAdaptiveActionButton(page); err == nil && nowSubmit {
			return true, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false, fmt.Errorf("seek adaptive: step did not advance — no progress-bar, heading or submit-button signal within 2s (prev=%d)", prevProgress)
}

func seekApplicationHeading(page *rod.Page) string {
	value, err := page.Eval(`()=>{` + jsLinkedInApplyContainerFn + `
  const scope=resolveApplyContainer();if(!scope)return '';
  const headings=[...scope.querySelectorAll('h1,h2,h3')].filter(el=>{const r=el.getBoundingClientRect();return r.width>0&&r.height>0;});
  return headings.length&&headings.length<=10?headings.map(el=>(el.textContent||'').replace(/\s+/g,' ').trim()).join('|'):'';
 }`)
	if err != nil || value == nil {
		return ""
	}
	return value.Value.String()
}

func (f *seekRodFiller) IsSubmitReady(ctx context.Context) (bool, error) {
	page := f.page.Context(ctx)
	_, isSubmit, err := seekAdaptiveActionButton(page)
	if err != nil || !isSubmit {
		return isSubmit, err
	}
	if err := seekVerifyFinalReviewDocuments(ctx, page, f.lazy); err != nil {
		return false, err
	}
	return true, nil
}

func (f *seekRodFiller) ClickSubmit(ctx context.Context) error {
	return f.ClickSubmitWithIntent(ctx, nil)
}

func (f *seekRodFiller) ClickSubmitWithIntent(ctx context.Context, persistIntent func() error) error {
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
	beforeClick := func(ctx context.Context) error {
		if f.beforeClick != nil {
			if err := f.beforeClick(ctx); err != nil {
				return err
			}
		}
		return seekVerifyFinalReviewDocuments(ctx, page, f.lazy)
	}
	if err := clickSeekAdaptiveActionWithIntent(ctx, page, true, beforeClick, persistIntent); err != nil {
		return fmt.Errorf("seek adaptive: click submit: %w", err)
	}
	return nil
}

func clickSeekAdaptiveAction(ctx context.Context, page *rod.Page, submit bool, beforeClick func(context.Context) error) error {
	return clickSeekAdaptiveActionWithIntent(ctx, page, submit, beforeClick, nil)
}

func clickSeekAdaptiveActionWithIntent(ctx context.Context, page *rod.Page, submit bool, beforeClick func(context.Context) error, persistIntent func() error) error {
	clickCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	page = page.Context(clickCtx)
	for refresh := 0; refresh < 3; refresh++ {
		button, currentSubmit, err := seekAdaptiveActionButton(page)
		if err != nil {
			return err
		}
		if button == nil || currentSubmit != submit {
			return fmt.Errorf("%w: navigation action changed before click", errAdaptiveQuestionReview)
		}
		if _, err := button.Eval(`()=>this.scrollIntoView({block:'center',inline:'center',behavior:'instant'})`); err != nil {
			return err
		}
		point, err := adaptiveTypeaheadClickPoint(clickCtx, page, button)
		if err != nil {
			return err
		}
		if err := moveBoundedMouse(clickCtx, page, *point); err != nil {
			return err
		}
		if beforeClick != nil {
			if err := beforeClick(clickCtx); err != nil {
				return err
			}
		}
		// Reacquire an equivalent action after hover hydration; a Next action
		// becoming Submit always holds before any native mouse press.
		current, err := button.Eval(`(submit)=>{const action=`+jsSeekAdaptiveAction+`;return action(false)===(submit?'submit':'next') && action(true)===this;}`, submit)
		if err != nil {
			return err
		}
		if current == nil || !current.Value.Bool() {
			continue
		}
		// Movement is safe when the current hit test still owns the control.
		// Dispatch once at its fresh point, rather than at the stale geometry.
		point, err = adaptiveTypeaheadClickPoint(clickCtx, page, button)
		if err != nil {
			return err
		}
		if beforeClick != nil {
			if err := beforeClick(clickCtx); err != nil {
				return err
			}
		}
		if !page.IsIframe() {
			point, err = probeLinkedInNativeClickPoint(clickCtx, button)
			if err != nil {
				continue
			}
		}
		current, err = button.Eval(`(submit)=>{const action=`+jsSeekAdaptiveAction+`;return action(false)===(submit?'submit':'next') && action(true)===this;}`, submit)
		if err != nil {
			return err
		}
		if current == nil || !current.Value.Bool() {
			continue
		}
		if err := clickCtx.Err(); err != nil {
			return err
		}
		if persistIntent != nil {
			if !submit {
				return errors.New("seek adaptive: submit intent cannot authorize navigation")
			}
			if err := persistIntent(); err != nil {
				return err
			}
			// A contended intent write can outlive the verified DOM. A changed
			// guard/control now holds as unknown without any dispatch retry.
			if beforeClick != nil {
				if err := beforeClick(clickCtx); err != nil {
					return err
				}
			}
			if !page.IsIframe() {
				point, err = probeLinkedInNativeClickPoint(clickCtx, button)
			} else {
				point, err = adaptiveTypeaheadClickPoint(clickCtx, page, button)
			}
			if err != nil {
				return err
			}
			current, err = button.Eval(`(submit)=>{const action=`+jsSeekAdaptiveAction+`;return action(false)===(submit?'submit':'next') && action(true)===this;}`, submit)
			if err != nil {
				return err
			}
			if current == nil || !current.Value.Bool() {
				return fmt.Errorf("%w: submit control changed during intent persistence", errAdaptiveQuestionReview)
			}
		}
		return clickBoundedMouse(clickCtx, page, *point)
	}
	return fmt.Errorf("%w: navigation control kept changing before dispatch", errAdaptiveQuestionReview)
}

const jsSeekAdaptiveAction = `(element)=>{` + jsLinkedInApplyContainerFn + `
 const scope=resolveApplyContainer();if(!scope)return element?null:'';
 const actionable=el=>{const r=el.getBoundingClientRect();if(!r.width||!r.height||el.disabled||el.getAttribute('aria-disabled')==='true')return false;for(let p=el;p;p=p.parentElement){const s=getComputedStyle(p);if(p.hidden||p.inert||p.getAttribute('aria-hidden')==='true'||s.display==='none'||s.visibility==='hidden')return false;}return true;};
 const buttons=allInDOM(scope,'button,input[type=submit],[role=button]').filter(actionable);
 const label=el=>(el.getAttribute('aria-label')||el.textContent||el.value||'').replace(/[\u2060\uFEFF\u200B-\u200D\u00AD]/g,'').replace(/\s+/g,' ').trim().toLowerCase();
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
		diagnostic, diagnosticErr := page.Eval(jsSeekNavigationDiagnostic)
		if diagnosticErr == nil {
			log.Warn().RawJSON("navigation_shape", []byte(diagnostic.Value.String())).Msg("seek: navigation control unresolved")
		}
		return nil, false, fmt.Errorf("seek adaptive: navigation control missing")
	}
	element, err := page.ElementByJS(rod.Eval(jsSeekAdaptiveAction, true))
	return element, kind == "submit", err
}

// Structural diagnostics contain no input values, applicant answers, or URL
// query strings. They make a changed jobsite form diagnosable from run logs.
const jsSeekNavigationDiagnostic = `()=>{` + jsLinkedInApplyContainerFn + `
 const meta=el=>({tag:el.tagName,testid:el.getAttribute('data-testid')||'',automation:el.getAttribute('data-automation')||'',role:el.getAttribute('role')||''});
 const visible=el=>{const r=el.getBoundingClientRect();return r.width>0&&r.height>0&&getComputedStyle(el).display!=='none'&&getComputedStyle(el).visibility!=='hidden';};
 const normalize=s=>(s||'').replace(/[\u2060\uFEFF\u200B-\u200D\u00AD]/g,'').replace(/\s+/g,' ').trim().toLowerCase();
 const controls=[...document.querySelectorAll('button,input[type=submit],[role=button]')].filter(visible).filter(el=>/^(continue|next|review|review application|save and continue|submit|submit application|send application)$/.test(normalize(el.getAttribute('aria-label')||el.textContent||el.value))).slice(0,10).map(el=>{
  const ancestors=[];for(let p=el.parentElement;p&&ancestors.length<15;p=p.parentElement)ancestors.push(meta(p));
  return {...meta(el),label:normalize(el.getAttribute('aria-label')||el.textContent||el.value),disabled:!!el.disabled,ancestors};
 });
 return JSON.stringify({scope_found:!!resolveApplyContainer(),native_apply_route:isNativeSeekApplyPage(),heading_count:[...document.querySelectorAll('h1')].filter(visible).length,landmarks:[...document.querySelectorAll('main,[role=main]')].filter(visible).slice(0,10).map(meta),forms:[...document.querySelectorAll('form')].filter(visible).slice(0,10).map(meta),controls});
}`

const jsSeekDocumentDiagnostic = `()=>{` + jsLinkedInApplyContainerFn + `
 const scope=resolveApplyContainer();
 const meta=el=>{const r=el.getBoundingClientRect(),s=getComputedStyle(el);return {tag:el.tagName,testid:el.getAttribute('data-testid')||'',role:el.getAttribute('role')||'',type:el.getAttribute('type')||'',in_scope:!!scope&&scope.contains(el),left:Math.round(r.left),right:Math.round(r.right),width:Math.round(r.width),height:Math.round(r.height),display:s.display,visibility:s.visibility,opacity:s.opacity};};
 const elements=[...document.querySelectorAll('input[type=file],input[name=resume-method],input[name=coverLetter-method],[data-testid=resumeFileInput],[data-testid=coverLetterFileInput]')].slice(0,12).map(el=>{
  const ancestors=[];for(let p=el.parentElement;p&&ancestors.length<12;p=p.parentElement)ancestors.push(meta(p));return {...meta(el),ancestors};
 });
 return JSON.stringify({scope_found:!!scope,viewport_width:window.innerWidth,scoped_file_inputs:scope?scope.querySelectorAll('input[type=file]').length:0,scoped_radios:scope?scope.querySelectorAll('input[type=radio],[role=radio]').length:0,elements});
}`
