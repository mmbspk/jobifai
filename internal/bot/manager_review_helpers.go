package bot

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/scraper"
)

func (m *Manager) pendingSubmitRequest(userID, jobID string) (SubmitRequest, error) {
	var req SubmitRequest
	req.JobID = jobID
	err := m.db.QueryRow(
		`SELECT company,role,COALESCE(location,''),platform,link,COALESCE(resume_path,''),COALESCE(cover_letter_path,''),
		        COALESCE(resume_content_version_id,''),COALESCE(cover_letter_content_version_id,''),COALESCE(document_refs_json,''),
		        COALESCE(suitability_reasoning,'')
		 FROM jobs_pending_review WHERE job_id = ? AND user_id = ?`, jobID, userID,
	).Scan(&req.Company, &req.Role, &req.Location, &req.Platform, &req.Link, &req.ResumePath, &req.CoverPath,
		&req.ResumeContentVersionID, &req.CoverContentVersionID, &req.DocumentRefsJSON, &req.SuitabilityReasoning)
	if err == sql.ErrNoRows {
		return SubmitRequest{}, fmt.Errorf("no pending review for job_id %s", jobID)
	}
	return req, err
}

func newManagerLazy(b *Bot, ctx context.Context, req SubmitRequest) *lazyDocGen {
	lazy := &lazyDocGen{
		b: b, ctx: ctx, job: linkedInJob{ID: req.JobID, Company: req.Company, Title: req.Role},
		resumeOverride: req.ResumePath, coverOverride: req.CoverPath,
		resumeVersionOverride: req.ResumeContentVersionID, coverVersionOverride: req.CoverContentVersionID,
		frozen: req.FrozenDocuments, refsJSON: req.DocumentRefsJSON,
	}
	lazy.applyStoredPackMetadata()
	lazy.restoreFrozenPack()
	return lazy
}

func (m *Manager) openReviewJobPage(ctx context.Context, b *Bot, gs domain.GeneralSettings, userID string, req SubmitRequest) (*rod.Page, bool, error) {
	var jobPage *rod.Page
	ownsBr := false
	if req.Platform == "seek" && gs.Browser.RemoteDebugPort == 0 {
		seekBr, brErr := m.getSeekBrowser(userID, gs)
		if brErr != nil {
			return nil, false, brErr
		}
		p, pageErr := seekBr.Page(proto.TargetCreateTarget{URL: req.Link})
		if pageErr != nil {
			m.InvalidateSeekBrowser(userID)
			return nil, false, pageErr
		}
		jobPage = p
	} else {
		newBr, newPage, launchErr := b.launchBrowser(ctx)
		if launchErr != nil {
			return nil, false, launchErr
		}
		if gs.Browser.RemoteDebugPort == 0 {
			ownsBr = true
		}
		if navErr := newPage.Navigate(req.Link); navErr != nil {
			_ = newPage.Close()
			if ownsBr {
				_ = newBr.Close()
			}
			return nil, false, navErr
		}
		jobPage = newPage
	}
	if req.Platform == "seek" {
		_ = jobPage.Timeout(30 * time.Second).WaitLoad()
		_ = jobPage.Timeout(5 * time.Second).WaitStable(500 * time.Millisecond)
	}
	return jobPage, ownsBr, nil
}

func managerLazyForSubmit(b *Bot, ctx context.Context, req SubmitRequest, jobPage *rod.Page) *lazyDocGen {
	jobDesc := req.Role + " at " + req.Company
	if req.Platform == "seek" {
		if rawHTML, err := jobPage.HTML(); err == nil {
			if d := scraper.ParseHTML(rawHTML); len(strings.TrimSpace(d.Description)) >= 100 {
				jobDesc = d.Description
			}
		}
	} else {
		managerDetails := b.fetchJob(ctx, linkedInJob{URL: req.Link, Company: req.Company, Title: req.Role})
		jobDesc = managerDetails.Description
	}
	lazy := newManagerLazy(b, ctx, req)
	lazy.jobDesc = jobDesc
	return lazy
}
