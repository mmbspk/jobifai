package documents

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/user/jobifai/internal/domain"
)

const (
	SourceJobTailored = "job_tailored"
	SourceJobCover    = "job_cover"
)

// MaterializeDeps supplies LLM tailoring and local PDF export (no LLM in render/reuse paths).
type MaterializeDeps struct {
	TailorProfile func(ctx context.Context, profile *domain.ResumeProfile, jobDesc string) (*domain.ResumeProfile, error)
	WriteCover    func(ctx context.Context, profile *domain.ResumeProfile, jobDesc string) (string, error)
	WritePDF      func(company, title, kind string, pdf []byte) string
}

// PreparedApplicationDocs holds upload paths and version ids for one application attempt.
type PreparedApplicationDocs struct {
	ResumePath, CoverPath         string
	ResumeVersionID, CoverVersionID string
	Resume, Cover                 ResolvedDocument
	Refs                          []domain.ApplicationDocumentRef
	Hold                          bool
	HoldReason                    string
}

// MaterializeApplicationDocs turns ResolveResult into local files and persisted content versions.
func (s *Service) MaterializeApplicationDocs(
	ctx context.Context,
	userID, company, title, jobDesc string,
	res ResolveResult,
	rc RenderContext,
	baseProfile *domain.ResumeProfile,
	deps MaterializeDeps,
) (PreparedApplicationDocs, error) {
	out := PreparedApplicationDocs{Resume: res.Resume, Cover: res.Cover}
	if res.Hold {
		out.Hold = true
		out.HoldReason = res.HoldReason
		return out, nil
	}
	docTitle := strings.TrimSpace(company + " — " + title)

	// Resume
	switch res.Resume.Outcome {
	case domain.DocumentOutcomeSiteHosted:
		// platform attachment only
	case domain.DocumentOutcomeSkipped:
	default:
		if res.Resume.NeedTailor {
			if baseProfile == nil {
				out.Hold = true
				out.HoldReason = "confirmed profile required"
				return out, nil
			}
			if deps.TailorProfile == nil {
				out.Hold = true
				out.HoldReason = "tailoring unavailable"
				return out, nil
			}
			tailored, err := deps.TailorProfile(ctx, baseProfile, jobDesc)
			if err != nil {
				return out, fmt.Errorf("tailor resume: %w", err)
			}
			vid, err := s.SaveResumeVersion(ctx, userID, "", docTitle, SourceJobTailored, tailored, rc)
			if err != nil {
				return out, err
			}
			res.Resume.VersionID = vid
			out.ResumeVersionID = vid
			pdf, _, err := s.PDFBytes(ctx, userID, vid)
			if err != nil {
				out.Hold = true
				out.HoldReason = "tailored resume PDF unavailable"
				return out, nil
			}
			if deps.WritePDF != nil {
				out.ResumePath = deps.WritePDF(company, title, "resume", pdf)
			}
		} else if res.Resume.VersionID != "" {
			out.ResumeVersionID = res.Resume.VersionID
			pdf, _, err := s.PDFBytes(ctx, userID, res.Resume.VersionID)
			if err != nil {
				if res.Resume.NeedDefaultPDF {
					out.Hold = true
					out.HoldReason = "default resume PDF unavailable"
					return out, nil
				}
				return out, err
			}
			if deps.WritePDF != nil && len(pdf) > 0 {
				out.ResumePath = deps.WritePDF(company, title, "resume", pdf)
			}
		}
	}

	// Cover
	switch res.Cover.Outcome {
	case domain.DocumentOutcomeSkipped, domain.DocumentOutcomeSiteHosted:
	default:
		if res.Cover.NeedGenerateCover {
			if baseProfile == nil || deps.WriteCover == nil {
				out.Hold = true
				out.HoldReason = "cover generation unavailable"
				return out, nil
			}
			body, err := deps.WriteCover(ctx, baseProfile, jobDesc)
			if err != nil {
				return out, fmt.Errorf("cover letter: %w", err)
			}
			vid, err := s.SaveCoverLetterWithSource(ctx, userID, docTitle+" cover", body, SourceJobCover, rc)
			if err != nil {
				return out, err
			}
			// re-tag source on version — SaveCoverLetter uses SourceUserEdit; acceptable for v1
			out.CoverVersionID = vid
			res.Cover.VersionID = vid
			pdf, _, err := s.PDFBytes(ctx, userID, vid)
			if err != nil {
				out.Hold = true
				out.HoldReason = "generated cover PDF unavailable"
				return out, nil
			}
			if deps.WritePDF != nil {
				out.CoverPath = deps.WritePDF(company, title, "cover_letter", pdf)
			}
		} else if res.Cover.VersionID != "" {
			out.CoverVersionID = res.Cover.VersionID
			pdf, _, err := s.PDFBytes(ctx, userID, res.Cover.VersionID)
			if err != nil {
				out.Hold = true
				out.HoldReason = "default cover PDF unavailable"
				return out, nil
			}
			if deps.WritePDF != nil && len(pdf) > 0 {
				out.CoverPath = deps.WritePDF(company, title, "cover_letter", pdf)
			}
		}
	}

	out.Refs = PackDocumentRefs(res.Resume, res.Cover, out.ResumePath, out.CoverPath)
	return out, nil
}

// WriteRefsJSON marshals application document refs for DB storage.
func WriteRefsJSON(refs []domain.ApplicationDocumentRef) string {
	if len(refs) == 0 {
		return ""
	}
	b, _ := json.Marshal(refs)
	return string(b)
}

// ExportPDFToJobDir writes pdf bytes under job_applications/ for platform upload (atomic temp file).
func ExportPDFToJobDir(root, company, title, kind string, pdf []byte) (string, error) {
	dir := filepath.Join(root, "job_applications", sanitizePath(company+"_"+title))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := "resume.pdf"
	if kind == "cover_letter" {
		name = "cover_letter.pdf"
	}
	dest := filepath.Join(dir, name)
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, pdf, 0o644); err != nil {
		return "", err
	}
	return dest, os.Rename(tmp, dest)
}

func sanitizePath(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' {
			return '_'
		}
		return r
	}, s)
	if s == "" {
		return "application"
	}
	return s
}
