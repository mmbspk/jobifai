package bot

import (
	"context"
	"fmt"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/rs/zerolog/log"
	"github.com/user/jobifai/internal/documents"
	"github.com/user/jobifai/internal/domain"
)

type easyApplyPrimaryClick func() (label string, ok bool, err error)

// scanApplyStepsForPrepare walks apply steps without uploading files or submitting.
func (b *Bot) scanApplyStepsForPrepare(ctx context.Context, page *rod.Page, lazy *lazyDocGen, clickPrimary easyApplyPrimaryClick) error {
	prevHash := ""
	for step := 0; step < 12; step++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		_, _, fillErr := b.fillFormStep(ctx, page, lazy)
		if fillErr != nil && !isFormFillFatalLLM(fillErr) {
			log.Warn().Err(fillErr).Int("step", step).Msg("prepare: form scan step warning")
		}
		if fillErr != nil && isFormFillFatalLLM(fillErr) {
			return fillErr
		}
		lazy.ensureMaterialized()
		if hashRes, err := page.Timeout(5*time.Second).Eval(`() => (document.body && document.body.innerText || '').substring(0, 200)`); err == nil {
			h := hashRes.Value.String()
			if h == prevHash && step > 0 {
				break
			}
			prevHash = h
		}
		label, ok, err := clickPrimary()
		if err != nil {
			log.Warn().Err(err).Int("step", step).Msg("prepare: primary button eval failed")
			break
		}
		if !ok || label == "Submit application" || label == "Submit" {
			break
		}
	}
	lazy.ensureMaterialized()
	return nil
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
	if err := b.seekClickQuickApply(page); err != nil {
		return err
	}
	if onLogin, err := b.seekWaitPastLogin(page); err != nil {
		return err
	} else if onLogin {
		return fmt.Errorf("seek session expired — re-login via Settings → Secrets")
	}
	if b.seekIsPostApplySuccess(page) {
		return fmt.Errorf("seek Quick Apply completed before the form could be scanned — apply manually or adjust document policies")
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
		if actionBtn == nil || isSubmit {
			break
		}
		if err := actionBtn.Click(proto.InputMouseButtonLeft, 1); err != nil {
			break
		}
	}
	lazy.ensureMaterialized()
	return nil
}
