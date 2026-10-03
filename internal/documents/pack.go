package documents

import (
	"encoding/json"
	"strings"

	"github.com/user/jobifai/internal/domain"
)

// ApplicationDocumentPack is the frozen application document state (paths, outcomes, provenance).
type ApplicationDocumentPack struct {
	HoldReason string                        `json:"hold_reason,omitempty"`
	Resume     domain.ApplicationDocumentRef `json:"resume,omitempty"`
	Cover      domain.ApplicationDocumentRef `json:"cover,omitempty"`
	Refs       []domain.ApplicationDocumentRef `json:"refs,omitempty"`
}

func (p ApplicationDocumentPack) ResumePath() string { return p.Resume.LocalPath }
func (p ApplicationDocumentPack) CoverPath() string  { return p.Cover.LocalPath }

// WriteApplicationPackJSON serializes a pack for jobs_pending_review / jobs_applied.
func WriteApplicationPackJSON(p ApplicationDocumentPack) string {
	if p.HoldReason == "" && p.Resume.Outcome == "" && p.Cover.Outcome == "" && len(p.Refs) == 0 {
		return ""
	}
	if len(p.Refs) == 0 {
		p.Refs = PackDocumentRefs(
			ResolvedDocument{Kind: KindResume, Outcome: p.Resume.Outcome, VersionID: p.Resume.ContentVersionID, UseSite: p.Resume.SiteHosted, HoldReason: p.Resume.HoldReason, PolicyMode: p.Resume.PolicyMode},
			ResolvedDocument{Kind: KindCoverLetter, Outcome: p.Cover.Outcome, VersionID: p.Cover.ContentVersionID, HoldReason: p.Cover.HoldReason, PolicyMode: p.Cover.PolicyMode},
			p.Resume.LocalPath, p.Cover.LocalPath,
		)
	}
	b, _ := json.Marshal(p)
	return string(b)
}

// ParseApplicationPackJSON reads a pack or legacy refs-only JSON array.
func ParseApplicationPackJSON(raw string) ApplicationDocumentPack {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ApplicationDocumentPack{}
	}
	if strings.HasPrefix(raw, "[") {
		var refs []domain.ApplicationDocumentRef
		if json.Unmarshal([]byte(raw), &refs) == nil {
			return packFromLegacyRefs(refs)
		}
		return ApplicationDocumentPack{}
	}
	var p ApplicationDocumentPack
	if json.Unmarshal([]byte(raw), &p) != nil {
		return ApplicationDocumentPack{}
	}
	if p.Resume.Outcome == "" || p.Cover.Outcome == "" {
		legacy := packFromLegacyRefs(p.Refs)
		if p.HoldReason != "" {
			legacy.HoldReason = p.HoldReason
		}
		if p.Resume.Outcome != "" {
			legacy.Resume = p.Resume
		}
		if p.Cover.Outcome != "" {
			legacy.Cover = p.Cover
		}
		return legacy
	}
	return p
}

func packFromLegacyRefs(refs []domain.ApplicationDocumentRef) ApplicationDocumentPack {
	p := ApplicationDocumentPack{Refs: refs}
	for _, r := range refs {
		switch r.Kind {
		case KindResume:
			p.Resume = r
		case KindCoverLetter:
			p.Cover = r
		}
	}
	return p
}

// ReadyForSubmit reports whether a stored pack can be reused without re-resolution.
func (p ApplicationDocumentPack) ReadyForSubmit() bool {
	if strings.Contains(p.HoldReason, "prepare after you approve") {
		return false
	}
	if p.Resume.Outcome != "" || p.Cover.Outcome != "" {
		return true
	}
	return p.HoldReason != "" && p.Resume.LocalPath != ""
}

func PackFromPrepared(holdReason string, resume, cover ResolvedDocument, resumePath, coverPath string) ApplicationDocumentPack {
	p := ApplicationDocumentPack{
		HoldReason: holdReason,
		Resume: domain.ApplicationDocumentRef{
			Kind: KindResume, Outcome: resume.Outcome, ContentVersionID: resume.VersionID,
			LocalPath: resumePath, SiteHosted: resume.UseSite, HoldReason: resume.HoldReason, PolicyMode: resume.PolicyMode,
		},
		Cover: domain.ApplicationDocumentRef{
			Kind: KindCoverLetter, Outcome: cover.Outcome, ContentVersionID: cover.VersionID,
			LocalPath: coverPath, HoldReason: cover.HoldReason, PolicyMode: cover.PolicyMode,
		},
	}
	p.Refs = PackDocumentRefs(resume, cover, resumePath, coverPath)
	return p
}
