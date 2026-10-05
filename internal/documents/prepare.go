package documents

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/user/jobifai/internal/domain"
)

const (
	SourceJobTailored = "job_tailored"
	SourceJobCover    = "job_cover"
)

// MaterializeDeps supplies LLM tailoring and export (render/reuse paths avoid LLM).
type MaterializeDeps struct {
	TailorProfile func(ctx context.Context, profile *domain.ResumeProfile, jobDesc string) (*domain.ResumeProfile, error)
	WriteCover    func(ctx context.Context, profile *domain.ResumeProfile, jobDesc string) (string, error)
	ExportUpload  func(userID, jobID, versionID, kind string, payload VersionUploadPayload) (string, error)
}

// PreparedApplicationDocs holds upload paths and version ids for one application attempt.
type PreparedApplicationDocs struct {
	ResumePath, CoverPath           string
	ResumeVersionID, CoverVersionID string
	Resume, Cover                   ResolvedDocument
	Pack                            ApplicationDocumentPack
	Hold                            bool
	HoldReason                      string
}

// MaterializeApplicationDocs turns ResolveResult into local files and persisted content versions.
func (s *Service) MaterializeApplicationDocs(
	ctx context.Context,
	userID, jobID, company, title, jobDesc string,
	res ResolveResult,
	policies domain.DocumentPolicies,
	rc RenderContext,
	baseProfile *domain.ResumeProfile,
	deps MaterializeDeps,
) (PreparedApplicationDocs, error) {
	if s.WorkGuard != nil {
		if err := s.WorkGuard.BeginWork(ctx, userID); err != nil {
			return PreparedApplicationDocs{}, err
		}
		defer s.WorkGuard.EndWork(userID)
	}
	out := PreparedApplicationDocs{Resume: res.Resume, Cover: res.Cover}
	if res.Hold {
		out.Hold = true
		out.HoldReason = res.HoldReason
		out.Pack = PackFromPrepared(out.HoldReason, res.Resume, res.Cover, "", "")
		return out, nil
	}
	docTitle := strings.TrimSpace(company + " — " + title)

	if err := s.materializeResume(ctx, userID, jobID, docTitle, jobDesc, rc, baseProfile, &res, &out, deps); err != nil {
		return out, err
	}
	if out.Hold {
		out.Pack = PackFromPrepared(out.HoldReason, res.Resume, res.Cover, out.ResumePath, out.CoverPath)
		return out, nil
	}
	if err := s.materializeCover(ctx, userID, jobID, docTitle, jobDesc, rc, baseProfile, policies, &res, &out, deps); err != nil {
		return out, err
	}
	if out.Hold {
		out.Pack = PackFromPrepared(out.HoldReason, res.Resume, res.Cover, out.ResumePath, out.CoverPath)
		return out, nil
	}

	out.Pack = PackFromPrepared("", res.Resume, res.Cover, out.ResumePath, out.CoverPath)
	return out, nil
}

func (s *Service) materializeResume(ctx context.Context, userID, jobID, docTitle, jobDesc string, rc RenderContext, baseProfile *domain.ResumeProfile, res *ResolveResult, out *PreparedApplicationDocs, deps MaterializeDeps) error {
	switch res.Resume.Outcome {
	case domain.DocumentOutcomeSiteHosted, domain.DocumentOutcomeSkipped:
		return nil
	}
	if res.Resume.NeedTailor {
		if baseProfile == nil {
			return out.failHold("confirmed profile required")
		}
		if deps.TailorProfile == nil {
			return out.failHold("tailoring unavailable")
		}
		tailored, err := deps.TailorProfile(ctx, baseProfile, jobDesc)
		if err != nil {
			return fmt.Errorf("tailor resume: %w", err)
		}
		vid, err := s.SaveResumeVersion(ctx, userID, "", docTitle, SourceJobTailored, tailored, rc)
		if err != nil {
			return err
		}
		res.Resume.VersionID = vid
		out.ResumeVersionID = vid
		return s.exportVersion(ctx, userID, jobID, vid, KindResume, deps, out, true)
	}
	if res.Resume.VersionID != "" {
		out.ResumeVersionID = res.Resume.VersionID
		return s.exportVersion(ctx, userID, jobID, res.Resume.VersionID, KindResume, deps, out, true)
	}
	return nil
}

func (s *Service) materializeCover(ctx context.Context, userID, jobID, docTitle, jobDesc string, rc RenderContext, baseProfile *domain.ResumeProfile, policies domain.DocumentPolicies, res *ResolveResult, out *PreparedApplicationDocs, deps MaterializeDeps) error {
	switch res.Cover.Outcome {
	case domain.DocumentOutcomeSiteHosted, domain.DocumentOutcomeSkipped:
		return nil
	}
	if res.Cover.NeedGenerateCover {
		if baseProfile == nil || deps.WriteCover == nil {
			return out.failHold("cover generation unavailable")
		}
		body, err := deps.WriteCover(ctx, baseProfile, jobDesc)
		if err != nil {
			if policies.Fallback.AllowGeneralCoverWhenGenerateFails {
				return s.tryGeneralCoverFallback(ctx, userID, jobID, policies, res, out, deps, err)
			}
			return fmt.Errorf("cover letter: %w", err)
		}
		vid, err := s.SaveCoverLetterWithSource(ctx, userID, docTitle+" cover", body, SourceJobCover, rc)
		if err != nil {
			if policies.Fallback.AllowGeneralCoverWhenGenerateFails {
				return s.tryGeneralCoverFallback(ctx, userID, jobID, policies, res, out, deps, err)
			}
			return err
		}
		out.CoverVersionID = vid
		res.Cover.VersionID = vid
		return s.exportVersion(ctx, userID, jobID, vid, KindCoverLetter, deps, out, false)
	}
	if res.Cover.VersionID != "" {
		out.CoverVersionID = res.Cover.VersionID
		return s.exportVersion(ctx, userID, jobID, res.Cover.VersionID, KindCoverLetter, deps, out, false)
	}
	return nil
}

func (s *Service) tryGeneralCoverFallback(ctx context.Context, userID, jobID string, policies domain.DocumentPolicies, res *ResolveResult, out *PreparedApplicationDocs, deps MaterializeDeps, cause error) error {
	list, err := s.List(ctx, userID)
	if err != nil || list.Defaults.CoverLetterVersionID == "" {
		return fmt.Errorf("cover letter: %w", cause)
	}
	res.Cover = ResolvedDocument{
		Kind: KindCoverLetter, Outcome: domain.DocumentOutcomeLocalFile,
		VersionID: list.Defaults.CoverLetterVersionID, NeedDefaultUpload: true, PolicyMode: "fallback_default",
	}
	out.CoverVersionID = list.Defaults.CoverLetterVersionID
	if err := s.exportVersion(ctx, userID, jobID, list.Defaults.CoverLetterVersionID, KindCoverLetter, deps, out, false); err != nil {
		return errors.Join(cause, err)
	}
	return nil
}

func (s *Service) exportVersion(ctx context.Context, userID, jobID, versionID, kind string, deps MaterializeDeps, out *PreparedApplicationDocs, resume bool) error {
	payload, err := s.UploadPayloadForVersion(ctx, userID, versionID)
	if err != nil {
		if resume {
			return out.failHold("resume file unavailable")
		}
		return out.failHold("cover file unavailable")
	}
	if !AllowedUploadMedia(payload.MediaType, true) {
		reason := "file type not accepted for upload"
		if resume {
			return out.failHold(reason)
		}
		return out.failHold(reason)
	}
	if deps.ExportUpload == nil {
		return out.failHold("export unavailable")
	}
	path, err := deps.ExportUpload(userID, jobID, versionID, kind, payload)
	if err != nil || path == "" {
		if resume {
			return out.failHold("resume export failed")
		}
		return out.failHold("cover export failed")
	}
	if resume {
		out.ResumePath = path
	} else {
		out.CoverPath = path
	}
	return nil
}

func (p *PreparedApplicationDocs) failHold(reason string) error {
	p.Hold = true
	p.HoldReason = reason
	p.Pack = PackFromPrepared(reason, p.Resume, p.Cover, p.ResumePath, p.CoverPath)
	return nil
}

// WriteRefsJSON is deprecated; prefer WriteApplicationPackJSON.
func WriteRefsJSON(refs []domain.ApplicationDocumentRef) string {
	if len(refs) == 0 {
		return ""
	}
	return WriteApplicationPackJSON(ApplicationDocumentPack{Refs: refs})
}
