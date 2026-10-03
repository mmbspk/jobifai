package documents

import (
	"strings"

	"github.com/user/jobifai/internal/domain"
)

// FormDocumentCapabilities describes what the application form supports for attachments.
type FormDocumentCapabilities struct {
	Detected            bool // true after form scan / capability probe
	ResumeFileSlots     int  // count of file inputs (0 when unknown)
	CoverRequired       bool
	CoverOptional       bool
	SiteResumePresent   bool
	SiteResumeAmbiguous bool
}

// ApplicationDocumentOverrides are explicit per-application choices (review UI / preflight).
type ApplicationDocumentOverrides struct {
	ResumeVersionID string
	CoverVersionID  string
	ResumeUseSite   bool
	CoverSkip       bool
	Frozen          bool
}

// ResolveInput bundles policy, defaults, overrides, and form capabilities.
type ResolveInput struct {
	Policies              domain.DocumentPolicies
	Defaults              DefaultsView
	EffectiveResumeVersionID string // global or regional default resume version
	Overrides             ApplicationDocumentOverrides
	Caps                  FormDocumentCapabilities
	EffectiveMarket       string
	HasConfirmedProfile     bool
	DefaultResumeExists     bool
	DefaultCoverExists      bool
}

// ResolvedDocument is the resolver output for one kind (resume or cover).
type ResolvedDocument struct {
	Kind              string
	Outcome           domain.DocumentResolutionOutcome
	VersionID         string
	HoldReason        string
	UseSite           bool
	Skip              bool
	NeedTailor        bool
	NeedDefaultUpload bool
	NeedGenerateCover bool
	PolicyMode        string
}

// ResolveResult is the paired resume/cover resolution. Hold is true when submission must stop.
type ResolveResult struct {
	Resume     ResolvedDocument
	Cover      ResolvedDocument
	Hold       bool
	HoldReason string
}

// Resolver selects documents per #59 priority: override → policy → authorised fallback → hold.
type Resolver struct{}

func (Resolver) Resolve(in ResolveInput) ResolveResult {
	res := ResolveResult{
		Resume: resolveResume(in),
		Cover:  resolveCover(in),
	}
	if res.Resume.Outcome == domain.DocumentOutcomeHoldReview {
		res.Hold = true
		res.HoldReason = firstNonEmpty(res.HoldReason, res.Resume.HoldReason)
	}
	if res.Cover.Outcome == domain.DocumentOutcomeHoldReview {
		res.Hold = true
		res.HoldReason = firstNonEmpty(res.HoldReason, res.Cover.HoldReason)
	}
	return res
}

func resolveResume(in ResolveInput) ResolvedDocument {
	const kind = KindResume
	if in.Overrides.Frozen {
		return frozenKind(kind, in.Overrides.ResumeVersionID)
	}
	if in.Overrides.ResumeUseSite {
		if in.Caps.SiteResumeAmbiguous {
			return holdDoc(kind, "site resume selection is ambiguous — verify manually", "application_override")
		}
		if in.Caps.Detected && !in.Caps.SiteResumePresent {
			return holdDoc(kind, "job-site resume mode but no site resume is attached", "application_override")
		}
		return ResolvedDocument{Kind: kind, Outcome: domain.DocumentOutcomeSiteHosted, UseSite: true, PolicyMode: "application_override"}
	}
	if vid := strings.TrimSpace(in.Overrides.ResumeVersionID); vid != "" {
		return ResolvedDocument{Kind: kind, Outcome: domain.DocumentOutcomeLocalFile, VersionID: vid, NeedDefaultUpload: true, PolicyMode: "application_override"}
	}
	mode := in.Policies.ResumeMode
	if mode == "" {
		mode = domain.ResumeDocumentModeDefault
	}
	if needsLocalResume(mode) && !in.Caps.Detected {
		return holdDoc(kind, "form document requirements unknown — scan apply form before preparing resume", string(mode))
	}
	switch mode {
	case domain.ResumeDocumentModeSiteHosted:
		if in.Caps.SiteResumeAmbiguous {
			return holdDoc(kind, "site resume selection is ambiguous — verify manually", string(mode))
		}
		if !in.Caps.SiteResumePresent {
			return holdDoc(kind, "job-site resume mode but no site resume is attached", string(mode))
		}
		return ResolvedDocument{Kind: kind, Outcome: domain.DocumentOutcomeSiteHosted, UseSite: true, PolicyMode: string(mode)}
	case domain.ResumeDocumentModeTailorJob:
		if !in.HasConfirmedProfile {
			return holdDoc(kind, "confirmed profile required for tailoring", string(mode))
		}
		return ResolvedDocument{Kind: kind, Outcome: domain.DocumentOutcomeLocalFile, NeedTailor: true, PolicyMode: string(mode)}
	case domain.ResumeDocumentModeDefault:
		vid := strings.TrimSpace(in.EffectiveResumeVersionID)
		if vid == "" {
			vid = strings.TrimSpace(in.Defaults.ResumeVersionID)
		}
		if vid != "" {
			return ResolvedDocument{Kind: kind, Outcome: domain.DocumentOutcomeLocalFile, VersionID: vid, NeedDefaultUpload: true, PolicyMode: string(mode)}
		}
		if in.Policies.Fallback.AllowSiteResumeWhenDefaultMissing && in.Caps.SiteResumePresent && !in.Caps.SiteResumeAmbiguous {
			return ResolvedDocument{Kind: kind, Outcome: domain.DocumentOutcomeSiteHosted, UseSite: true, PolicyMode: "fallback_site"}
		}
		if !in.Policies.OnboardingComplete && !in.DefaultResumeExists {
			return holdDoc(kind, "select a default resume in Documents before applying", string(mode))
		}
		return holdDoc(kind, "no default resume selected", string(mode))
	default:
		return holdDoc(kind, "unknown resume policy", string(mode))
	}
}

func resolveCover(in ResolveInput) ResolvedDocument {
	const kind = KindCoverLetter
	if in.Overrides.Frozen {
		return frozenKind(kind, in.Overrides.CoverVersionID)
	}
	if in.Overrides.CoverSkip {
		if in.Caps.Detected && in.Caps.CoverRequired {
			return holdDoc(kind, "cover letter required by form but application skips cover", "application_override")
		}
		return ResolvedDocument{Kind: kind, Outcome: domain.DocumentOutcomeSkipped, Skip: true, PolicyMode: "application_override"}
	}
	if vid := strings.TrimSpace(in.Overrides.CoverVersionID); vid != "" {
		return ResolvedDocument{Kind: kind, Outcome: domain.DocumentOutcomeLocalFile, VersionID: vid, NeedDefaultUpload: true, PolicyMode: "application_override"}
	}
	mode := in.Policies.CoverMode
	if mode == "" {
		mode = domain.CoverDocumentModeWhenRequired
	}
	want, needCaps := coverIntent(mode, in.Caps)
	if needCaps && !in.Caps.Detected {
		return holdDoc(kind, "form document requirements unknown — scan apply form before preparing cover letter", string(mode))
	}
	if !want {
		return ResolvedDocument{Kind: kind, Outcome: domain.DocumentOutcomeSkipped, Skip: true, PolicyMode: string(mode)}
	}
	switch mode {
	case domain.CoverDocumentModeGeneralDefault:
		if in.Defaults.CoverLetterVersionID != "" {
			return ResolvedDocument{Kind: kind, Outcome: domain.DocumentOutcomeLocalFile, VersionID: in.Defaults.CoverLetterVersionID, NeedDefaultUpload: true, PolicyMode: string(mode)}
		}
		return holdDoc(kind, "no general cover letter default selected", string(mode))
	case domain.CoverDocumentModeWhenRequired, domain.CoverDocumentModeWhenAccepted:
		if !in.HasConfirmedProfile {
			return holdDoc(kind, "confirmed profile required to generate cover letter", string(mode))
		}
		return ResolvedDocument{Kind: kind, Outcome: domain.DocumentOutcomeLocalFile, NeedGenerateCover: true, PolicyMode: string(mode)}
	case domain.CoverDocumentModeSkipOptional:
		if in.Caps.CoverRequired {
			return holdDoc(kind, "cover letter required by form but policy skips optional covers", string(mode))
		}
		return ResolvedDocument{Kind: kind, Outcome: domain.DocumentOutcomeSkipped, Skip: true, PolicyMode: string(mode)}
	default:
		return holdDoc(kind, "unknown cover policy", string(mode))
	}
}

// coverIntent returns whether cover action is needed and whether capabilities must be known first.
func coverIntent(mode domain.CoverDocumentMode, caps FormDocumentCapabilities) (want, needCaps bool) {
	if !caps.Detected {
		return false, true
	}
	switch mode {
	case domain.CoverDocumentModeWhenRequired:
		return caps.CoverRequired, true
	case domain.CoverDocumentModeWhenAccepted:
		return caps.CoverOptional || caps.CoverRequired || caps.ResumeFileSlots >= 2, true
	case domain.CoverDocumentModeGeneralDefault:
		return caps.CoverRequired || caps.CoverOptional || caps.ResumeFileSlots >= 2, true
	case domain.CoverDocumentModeSkipOptional:
		return caps.CoverRequired, true
	default:
		return caps.CoverRequired, true
	}
}

func needsLocalResume(mode domain.ResumeDocumentMode) bool {
	return mode == domain.ResumeDocumentModeDefault || mode == domain.ResumeDocumentModeTailorJob
}

func frozenKind(kind, versionID string) ResolvedDocument {
	vid := strings.TrimSpace(versionID)
	if vid == "" {
		return ResolvedDocument{Kind: kind, Outcome: domain.DocumentOutcomeSkipped, Skip: true, PolicyMode: "frozen_override"}
	}
	return ResolvedDocument{Kind: kind, Outcome: domain.DocumentOutcomeLocalFile, VersionID: vid, NeedDefaultUpload: true, PolicyMode: "frozen_override"}
}

func holdDoc(kind, reason, mode string) ResolvedDocument {
	return ResolvedDocument{
		Kind:       kind,
		Outcome:    domain.DocumentOutcomeHoldReview,
		HoldReason: reason,
		PolicyMode: mode,
	}
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// PackDocumentRefs builds JSON-ready refs from a prepared application pack.
func PackDocumentRefs(resume, cover ResolvedDocument, resumePath, coverPath string) []domain.ApplicationDocumentRef {
	out := make([]domain.ApplicationDocumentRef, 0, 2)
	if resume.Outcome != domain.DocumentOutcomeSkipped {
		out = append(out, domain.ApplicationDocumentRef{
			Kind: KindResume, Outcome: resume.Outcome, ContentVersionID: resume.VersionID,
			LocalPath: resumePath, SiteHosted: resume.UseSite, HoldReason: resume.HoldReason, PolicyMode: resume.PolicyMode,
		})
	}
	if cover.Outcome != domain.DocumentOutcomeSkipped {
		out = append(out, domain.ApplicationDocumentRef{
			Kind: KindCoverLetter, Outcome: cover.Outcome, ContentVersionID: cover.VersionID,
			LocalPath: coverPath, PolicyMode: cover.PolicyMode,
		})
	}
	return out
}
