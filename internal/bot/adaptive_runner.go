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
	if ac != nil && ac.Engine == domain.ApplyEngineShadow {
		return shadowEasyApply()
	}

	// Pre-flight checks identical to legacy easyApply.
	if lazy == nil {
		return fmt.Errorf("%w: missing declared job", errLinkedInIdentity)
	}
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
	modalDeadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(modalDeadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		firstFields, firstPage, _ = b.scanAdaptiveStep(ctx, page)
		if len(firstFields) > 0 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if len(firstFields) == 0 {
		return fmt.Errorf("adaptive: modal did not render fields within timeout")
	}
	activePage := page
	if firstPage != nil {
		activePage = firstPage
	}

	b.log.Info().Str("attempt_id", lazy.attemptID).Int("initial_fields", len(firstFields)).Msg("adaptive: modal ready, entering step loop")

	// Set up the adaptive pipeline components.
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
	filler := &linkedInRodFiller{page: activePage, bot: b}

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

		// Interpret: deterministic → mapping → LLM.
		interp, err := si.InterpretStep(ctx, snap, profile)
		if err != nil {
			return fmt.Errorf("adaptive: interpret step %d: %w", stepIdx, err)
		}

		if interp.Paused {
			log.Warn().Int("step", stepIdx).Str("reason", string(interp.PauseReason)).Msg("adaptive: step paused, needs human review")
			return fmt.Errorf("adaptive: step %d paused: %s", stepIdx, interp.PauseReason)
		}

		// Execute: fill fields + navigate.
		result, err := se.ExecuteStep(ctx, snap, interp, filler)
		if err != nil && result.Outcome != StepOutcomeSubmitted {
			return fmt.Errorf("adaptive: execute step %d: %w", stepIdx, err)
		}

		// Persist verified resolutions to the mapping store (best-effort).
		if b.cfg.AdaptiveMapping != nil {
			_ = b.cfg.AdaptiveMapping.Write(ctx, snap, interp.Fields)
		}

		switch result.Outcome {
		case StepOutcomeSubmitted:
			submitClicked = true
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
		res, err := p.Timeout(10 * time.Second).Eval(jsScanFields, map[string]interface{}{})
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

// ── linkedInRodFiller ─────────────────────────────────────────────────────────

// linkedInRodFiller implements FieldFiller for LinkedIn Easy Apply using the
// existing Rod-based JS helpers (jsFillByQuestionAnswer, jsClickApplyNavigation).
type linkedInRodFiller struct {
	page *rod.Page
	bot  *Bot
}

func (f *linkedInRodFiller) Fill(_ context.Context, field SnapField, res FieldResolution) FieldFillResult {
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
			return FieldFillResult{Err: fmt.Errorf("adaptive: file upload slot not found (ref %s)", field.Ref)}
		}
		return FieldFillResult{Filled: true}
	}

	// ActionFill: use jsFillByQuestionAnswer for text/radio/select/textarea.
	var fieldTypeParm string
	switch field.Type {
	case SnapTypeRadio, SnapTypeSelect:
		fieldTypeParm = "radio"
	default:
		fieldTypeParm = "text"
	}
	result, err := f.page.Eval(jsFillByQuestionAnswer, field.Label, res.Value, fieldTypeParm)
	if err != nil {
		return FieldFillResult{Err: fmt.Errorf("adaptive: fill field %s (%q): %w", field.Ref, field.Label, err)}
	}
	if !result.Value.Bool() {
		return FieldFillResult{Err: fmt.Errorf("adaptive: fill returned false for field %s (%q)", field.Ref, field.Label)}
	}
	return FieldFillResult{Filled: true}
}

func (f *linkedInRodFiller) NavigateNext(_ context.Context) (bool, error) {
	_, ok, err := f.bot.clickEasyApplyNavigation(f.page)
	return ok, err
}

func (f *linkedInRodFiller) IsSubmitReady(_ context.Context) (bool, error) {
	_, kind, found, _, err := f.bot.peekEasyApplyPrimary(f.page)
	if err != nil || !found {
		return false, nil
	}
	return applyPrimaryIsSubmit(kind), nil
}

func (f *linkedInRodFiller) ClickSubmit(_ context.Context) error {
	_, ok, err := f.bot.clickEasyApplyNavigation(f.page)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("adaptive: submit click returned not-ok")
	}
	return nil
}

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
	if ac != nil && ac.Engine == domain.ApplyEngineShadow {
		return shadowSeekApply()
	}
	if lazy == nil {
		return fmt.Errorf("seek adaptive: missing declared job: %w", errLinkedInIdentity)
	}

	// Wait for the job page to be fully loaded.
	if ready, err := page.Eval(`() => document.readyState`); err != nil || ready.Value.String() != "complete" {
		if err := page.Timeout(30 * time.Second).WaitLoad(); err != nil {
			return fmt.Errorf("seek adaptive: wait load: %w", err)
		}
	}
	_ = page.Timeout(5 * time.Second).WaitStable(2 * time.Second)

	// Persist session tokens before any navigation so a fresh rotate is recorded.
	b.seekPersistSession(page)

	// Verify the session is authenticated.
	if err := b.seekEnsureLoggedIn(page); err != nil {
		return fmt.Errorf("seek adaptive: session expired — re-add Seek credentials: %w", err)
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
		LLM:     b.cfg.AdaptiveLLM,
		Mapping: b.cfg.AdaptiveMapping,
	}
	guard := &SubmitGuard{
		DB:        b.cfg.DB,
		AttemptID: lazy.attemptID,
		UserID:    b.cfg.UserID,
	}
	se := NewStepExecutor(DefaultStepExecBounds(), guard)
	filler := &seekRodFiller{page: page, bot: b}

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

		interp, err := si.InterpretStep(ctx, snap, profile)
		if err != nil {
			return fmt.Errorf("seek adaptive: interpret step %d: %w", stepIdx, err)
		}
		if interp.Paused {
			return fmt.Errorf("seek adaptive: step %d paused: %s", stepIdx, interp.PauseReason)
		}

		result, err := se.ExecuteStep(ctx, snap, interp, filler)
		if err != nil && result.Outcome != StepOutcomeSubmitted {
			return fmt.Errorf("seek adaptive: execute step %d: %w", stepIdx, err)
		}

		if b.cfg.AdaptiveMapping != nil {
			_ = b.cfg.AdaptiveMapping.Write(ctx, snap, interp.Fields)
		}

		switch result.Outcome {
		case StepOutcomeSubmitted:
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
}

func (f *seekRodFiller) Fill(_ context.Context, field SnapField, res FieldResolution) FieldFillResult {
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
			return FieldFillResult{Err: fmt.Errorf("seek adaptive: file upload slot not found (ref %s)", field.Ref)}
		}
		return FieldFillResult{Filled: true}
	}
	var fieldTypeParm string
	switch field.Type {
	case SnapTypeRadio, SnapTypeSelect:
		fieldTypeParm = "radio"
	default:
		fieldTypeParm = "text"
	}
	result, err := f.page.Eval(jsFillByQuestionAnswer, field.Label, res.Value, fieldTypeParm)
	if err != nil {
		return FieldFillResult{Err: fmt.Errorf("seek adaptive: fill field %s (%q): %w", field.Ref, field.Label, err)}
	}
	if !result.Value.Bool() {
		return FieldFillResult{Err: fmt.Errorf("seek adaptive: fill returned false for field %s (%q)", field.Ref, field.Label)}
	}
	return FieldFillResult{Filled: true}
}

func (f *seekRodFiller) NavigateNext(_ context.Context) (bool, error) {
	btn, isSubmit := f.bot.seekFindActionButton(f.page)
	if btn == nil || isSubmit {
		return false, nil
	}
	if err := btn.Click(proto.InputMouseButtonLeft, 1); err != nil {
		return false, fmt.Errorf("seek adaptive: click continue: %w", err)
	}
	time.Sleep(400 * time.Millisecond)
	return true, nil
}

func (f *seekRodFiller) IsSubmitReady(_ context.Context) (bool, error) {
	_, isSubmit := f.bot.seekFindActionButton(f.page)
	return isSubmit, nil
}

func (f *seekRodFiller) ClickSubmit(_ context.Context) error {
	btn, isSubmit := f.bot.seekFindActionButton(f.page)
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
