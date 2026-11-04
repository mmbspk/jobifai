package bot

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/rs/zerolog/log"
	"github.com/user/jobifai/internal/documents"
	"github.com/user/jobifai/internal/domain"
)

// ErrPrepareSuperseded is returned when a prepare run terminal write affects zero rows
// because another run has reclaimed the job (its prepID no longer matches prepare_status).
// The handler treats this as a no-op rather than a success or failure.
var ErrPrepareSuperseded = errors.New("prepare: run superseded by a newer preparation")

// scanApplyStepsForPrepare fills fields and prepares attachments, but never submits.
func (b *Bot) scanApplyStepsForPrepare(ctx context.Context, page *rod.Page, lazy *lazyDocGen) error {
	if lazy == nil {
		return fmt.Errorf("%w: missing declared job", errLinkedInIdentity)
	}
	expectedPath := linkedInJobPath(lazy.job.URL)
	prevStepHash := ""
	stuckSteps := 0
	for step := 0; step < 12; step++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err := verifyJobIdentity(page, expectedPath); err != nil {
			return err
		}
		_, _, fillErr := b.fillFormStep(ctx, page, lazy)
		// A failed upload must not be hidden by navigation to the review page,
		// which would retain whatever attachment LinkedIn previously selected.
		if fillErr != nil {
			b.log.Warn().Err(fillErr).Int("step", step).Msg("prepare: form step failed")
			return fmt.Errorf("prepare: form step %d failed: %w", step+1, fillErr)
		}
		if lazy.holdStage != "" {
			return fmt.Errorf("prepare: %s", lazy.holdReason)
		}
		lazy.ensureMaterialized()

		label, kind, found, enabled, peekErr := b.peekEasyApplyPrimary(page)
		if peekErr != nil {
			return fmt.Errorf("prepare: scan incomplete — could not read apply buttons: %w", peekErr)
		}
		atFinalReview := b.linkedInPrepareAtFinalReview(page)
		if linkedInPrepareScanComplete(kind, atFinalReview) {
			if atFinalReview && !lazy.policyBlocked() {
				if resumePath, _ := lazy.peek(); resumePath != "" {
					if verifyErr := b.linkedInVerifyFinalReviewResume(page, resumePath); verifyErr != nil {
						lazy.holdReason = verifyErr.Error()
						lazy.holdStage = uploadStageFinalReviewMismatch
						lazy.holdFile = filepath.Base(resumePath)
					}
				}
			}
			if !lazy.policyBlocked() {
				lazy.prepareScanComplete = true
			}
			break
		}
		if !found {
			return fmt.Errorf("prepare: scan incomplete — no apply navigation at step %d", step+1)
		}
		if !enabled {
			return fmt.Errorf("prepare: scan incomplete — apply button disabled (%s)", label)
		}
		if !applyPrimaryAllowsPrepareNav(kind) {
			return fmt.Errorf("prepare: unsupported apply action %q during document prepare", label)
		}

		stepHash := b.linkedInApplyStepHash(page)
		if stepHash != "" && stepHash == prevStepHash {
			stuckSteps++
			if stuckSteps >= 2 {
				return fmt.Errorf("prepare: scan incomplete — form did not advance at step %d", step+1)
			}
		} else {
			stuckSteps = 0
			prevStepHash = stepHash
		}

		if err := verifyJobIdentity(page, expectedPath); err != nil {
			return err
		}
		navLabel, clicked, clickErr := b.clickEasyApplyNavigation(page)
		if clickErr != nil {
			return fmt.Errorf("prepare: scan incomplete — navigation click failed: %w", clickErr)
		}
		if !clicked {
			return fmt.Errorf("prepare: scan incomplete — could not advance (%s)", navLabel)
		}
		log.Debug().Str("label", navLabel).Int("step", step).Msg("prepare: advanced apply step")
	}
	lazy.ensureMaterialized()
	if !lazy.prepareScanComplete {
		if lazy.policyBlocked() {
			return fmt.Errorf("prepare: %s", lazy.holdReason)
		}
		return fmt.Errorf("prepare: scan incomplete — review step not reached")
	}
	return nil
}

func (b *Bot) linkedInVerifyFinalReviewResume(page *rod.Page, resumePath string) error {
	base := filepath.Base(resumePath)
	res, err := page.Timeout(5*time.Second).Eval(jsLinkedInFinalReviewResumeCheck, base)
	if err != nil {
		return fmt.Errorf("linkedin: final-review resume verification failed: %w", err)
	}
	if !res.Value.Get("verified").Bool() {
		if observed := res.Value.Get("observedFilename").String(); observed != "" {
			return fmt.Errorf("linkedin: resume %q not confirmed in final-review summary — found %q", base, observed)
		}
		return fmt.Errorf("linkedin: resume %q not confirmed in final-review summary", base)
	}
	return nil
}

func (b *Bot) linkedInApplyStepHash(page *rod.Page) string {
	res, err := page.Eval(`() => {
		const t = (document.body && document.body.innerText || '').substring(0, 400);
		return t.replace(/\s+/g, ' ').trim();
	}`)
	if err != nil {
		return ""
	}
	return res.Value.String()
}

// PrepareReviewDocuments opens the apply form, scans capabilities, and materializes documents.
// prepID identifies this preparation run; it gates every terminal DB write so a superseded
// run (e.g. one reclaimed as stale) can never overwrite a newer run's persisted pack.
func (m *Manager) PrepareReviewDocuments(ctx context.Context, userID, jobID, prepID string) (retErr error) {
	if m.submitWorkGuard != nil {
		if err := m.submitWorkGuard.BeginWork(ctx, userID); err != nil {
			return err
		}
		defer m.submitWorkGuard.EndWork(userID)
	}
	req, err := m.pendingSubmitRequest(userID, jobID)
	if err != nil {
		return err
	}
	b, gs, err := m.setupBot(userID, domain.Platform(req.Platform), "")
	if err != nil {
		return err
	}
	if b.cfg.Documents == nil {
		return fmt.Errorf("document policies are not enabled for this user")
	}

	// Register this preparation as a run so its structured diagnostics reach the persistent
	// log sink (bot_run_logs), not just stdout. Inherits an active search run if one exists.
	runID, cleanupRun, acqErr := m.acquireRun(ctx, userID, "prepare-"+prepID)
	if acqErr != nil {
		return fmt.Errorf("prepare: register run log sink: %w", acqErr)
	}
	defer cleanupRun()
	b.cfg.AutomationRunID = runID
	b.log = log.Logger.With().
		Str("user_id", userID).Str("automation_run_id", runID).
		Str("job_id", jobID).Str("prep_id", prepID).Logger().
		Hook(logIDHook{})
	b.log.Info().Msg("prepare: run started")

	jobPage, ownsBr, err := m.openReviewJobPage(ctx, b, gs, userID, req)
	if err != nil {
		return err
	}
	defer func() { _ = jobPage.Close() }()
	if ownsBr {
		defer func() {
			if br := jobPage.Browser(); br != nil {
				_ = br.Close()
			}
		}()
	}

	lazy := managerLazyForSubmit(b, m.ctx, req, jobPage)
	lazy.reviewPrepareOnly = true
	defer func() { lazy.reviewPrepareOnly = false }()

	// Terminal event: always close the prepare run with a durable, structured outcome line.
	defer func() {
		ev := b.log.Info()
		if retErr != nil || lazy.holdReason != "" {
			ev = b.log.Warn()
		}
		if retErr != nil {
			ev = ev.Err(retErr)
		}
		ev.Str("stage", lazy.holdStage).Str("hold_file", lazy.holdFile).
			Str("hold_reason", lazy.holdReason).Bool("scan_complete", lazy.prepareScanComplete).
			Msg("prepare: run finished")
	}()

	var prepErr error
	if req.Platform == "seek" {
		prepErr = m.prepareSeekReviewDocuments(ctx, b, jobPage, lazy, req.Link)
	} else {
		prepErr = b.easyApply(ctx, jobPage, lazy)
	}
	if prepErr != nil {
		if !errors.Is(prepErr, context.Canceled) && !errors.Is(prepErr, context.DeadlineExceeded) {
			pack := documents.ParseApplicationPackJSON(lazy.packJSONForPersist())
			pack.HoldReason = prepErr.Error()
			// Preserve runtime capabilities + selection established before the hold so the UI
			// shows real form requirements rather than "unknown", and record the stable stage.
			if lazy.formCaps.Detected {
				pack.FormCaps = lazy.formCaps
			}
			sel := documents.ApplicationDocumentSelection{
				ResumeVersionID: lazy.resumeVersionOverride,
				CoverVersionID:  lazy.coverVersionOverride,
				ResumeUseSite:   lazy.resumeUseSiteOverride,
				CoverSkip:       lazy.coverSkipOverride,
			}
			if sel != (documents.ApplicationDocumentSelection{}) {
				pack.Selection = sel
			}
			if lazy.holdStage != "" {
				pack.HoldStage = lazy.holdStage
				pack.HoldFile = lazy.holdFile
			}
			if packJSON := documents.WriteApplicationPackJSON(pack); packJSON != "" {
				dbCtx, dbCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer dbCancel()
				if _, dbErr := m.db.ExecContext(dbCtx,
					`UPDATE jobs_pending_review SET document_refs_json=?
					 WHERE job_id=? AND user_id=? AND prepare_status = 'running:' || ?`,
					packJSON, jobID, userID, prepID); dbErr != nil {
					log.Warn().Err(dbErr).Str("job_id", jobID).Msg("prepare: failed to persist error to document_refs_json")
				}
			}
		}
		return prepErr
	}
	if !lazy.prepareScanComplete {
		return fmt.Errorf("prepare: scan incomplete — review step not reached")
	}

	pack := documents.ParseApplicationPackJSON(lazy.packJSONForPersist())
	if lazy.policyBlocked() {
		pack.HoldReason = lazy.holdReason
		if lazy.holdStage != "" {
			pack.HoldStage = lazy.holdStage
			pack.HoldFile = lazy.holdFile
		}
	} else {
		pack.MarkPrepared()
	}
	pack.FormCaps = lazy.formCaps
	pack.Selection = documents.ApplicationDocumentSelection{
		ResumeVersionID: lazy.resumeVersionOverride,
		CoverVersionID:  lazy.coverVersionOverride,
		ResumeUseSite:   lazy.resumeUseSiteOverride,
		CoverSkip:       lazy.coverSkipOverride,
	}
	resumePath, coverPath := lazy.peek()
	pack.Resume.LocalPath = resumePath
	pack.Cover.LocalPath = coverPath
	packJSON := documents.WriteApplicationPackJSON(pack)
	if packJSON == "" {
		packJSON = lazy.packJSONForPersist()
	}
	res, execErr := m.db.ExecContext(ctx,
		`UPDATE jobs_pending_review
		 SET resume_path = ?, cover_letter_path = ?,
		     resume_content_version_id = ?, cover_letter_content_version_id = ?,
		     document_refs_json = ?
		 WHERE job_id = ? AND user_id = ? AND prepare_status = 'running:' || ?`,
		resumePath, coverPath, lazy.resumeVersionID, lazy.coverVersionID, packJSON, jobID, userID, prepID,
	)
	if execErr != nil {
		return execErr
	}
	// A zero-row update means this run was superseded (another prepare reclaimed the job).
	// Report that so the handler does not mark a stale run as succeeded.
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrPrepareSuperseded
	}
	return nil
}

func (m *Manager) prepareSeekReviewDocuments(ctx context.Context, b *Bot, page *rod.Page, lazy *lazyDocGen, jobURL string) error {
	if err := b.seekOpenQuickApplyForm(page, true); err != nil {
		return err
	}
	if onLogin, err := b.seekWaitPastLogin(page); err != nil {
		return err
	} else if onLogin {
		return fmt.Errorf("seek session expired — re-login via Settings → Secrets")
	}
	if b.seekIsPostApplySuccess(page) {
		return fmt.Errorf("seek Quick Apply submitted before the form could be scanned — apply manually on Seek")
	}
	if err := b.ensureSeekApplyFormVisible(page, jobURL); err != nil {
		return err
	}
	for step := 0; step < 10; step++ {
		_, _, fillErr := b.fillFormStep(ctx, page, lazy)
		if fillErr != nil && isFormFillFatalLLM(fillErr) {
			return fillErr
		}
		lazy.applyCaps(probeApplyFormCaps(page))
		actionBtn, isSubmit := b.seekFindActionButton(page)
		if actionBtn == nil {
			return fmt.Errorf("prepare: scan incomplete — no continue button at step %d", step+1)
		}
		if isSubmit {
			lazy.prepareScanComplete = true
			break
		}
		if err := actionBtn.Click(proto.InputMouseButtonLeft, 1); err != nil {
			return fmt.Errorf("prepare: scan incomplete — could not advance at step %d: %w", step+1, err)
		}
	}
	lazy.ensureMaterialized()
	if !lazy.prepareScanComplete {
		return fmt.Errorf("prepare: scan incomplete — review step not reached")
	}
	return nil
}
