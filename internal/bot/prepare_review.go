package bot

import (
	"context"
	"fmt"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/rs/zerolog/log"
	"github.com/user/jobifai/internal/documents"
	"github.com/user/jobifai/internal/domain"
)

// scanApplyStepsForPrepare walks apply steps without uploading files or submitting.
func (b *Bot) scanApplyStepsForPrepare(ctx context.Context, page *rod.Page, lazy *lazyDocGen) error {
	prevStepHash := ""
	stuckSteps := 0
	for step := 0; step < 12; step++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		_, _, fillErr := b.fillFormStep(ctx, page, lazy)
		if fillErr != nil && isFormFillFatalLLM(fillErr) {
			return fillErr
		}
		if fillErr != nil {
			log.Warn().Err(fillErr).Int("step", step).Msg("prepare: form scan step warning")
		}
		lazy.ensureMaterialized()

		label, kind, found, enabled, peekErr := b.peekEasyApplyPrimary(page)
		if peekErr != nil {
			return fmt.Errorf("prepare: scan incomplete — could not read apply buttons: %w", peekErr)
		}
		atFinalReview := b.linkedInPrepareAtFinalReview(page)
		if linkedInPrepareScanComplete(kind, atFinalReview) {
			lazy.prepareScanComplete = true
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
		return fmt.Errorf("prepare: scan incomplete — review step not reached")
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
func (m *Manager) PrepareReviewDocuments(ctx context.Context, userID, jobID string) error {
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

	var prepErr error
	if req.Platform == "seek" {
		prepErr = m.prepareSeekReviewDocuments(ctx, b, jobPage, lazy, req.Link)
	} else {
		prepErr = b.easyApply(ctx, jobPage, lazy)
	}
	if prepErr != nil {
		return prepErr
	}
	if !lazy.prepareScanComplete {
		return fmt.Errorf("prepare: scan incomplete — review step not reached")
	}

	pack := documents.ParseApplicationPackJSON(lazy.packJSONForPersist())
	if lazy.policyBlocked() {
		pack.HoldReason = lazy.holdReason
	} else {
		pack.MarkPrepared()
	}
	resumePath, coverPath := lazy.peek()
	pack.Resume.LocalPath = resumePath
	pack.Cover.LocalPath = coverPath
	packJSON := documents.WriteApplicationPackJSON(pack)
	if packJSON == "" {
		packJSON = lazy.packJSONForPersist()
	}
	_, err = m.db.ExecContext(ctx,
		`UPDATE jobs_pending_review
		 SET resume_path = ?, cover_letter_path = ?,
		     resume_content_version_id = ?, cover_letter_content_version_id = ?,
		     document_refs_json = ?
		 WHERE job_id = ? AND user_id = ?`,
		resumePath, coverPath, lazy.resumeVersionID, lazy.coverVersionID, packJSON, jobID, userID,
	)
	return err
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
