package documents

import (
	"context"
	"errors"
	"strings"

	"github.com/user/jobifai/internal/domain"
)

// ApplicationDocumentSelection is the per-application review picker state.
type ApplicationDocumentSelection struct {
	ResumeVersionID string `json:"resume_version_id,omitempty"`
	CoverVersionID  string `json:"cover_version_id,omitempty"`
	ResumeUseSite   bool   `json:"resume_use_site,omitempty"`
	CoverSkip       bool   `json:"cover_skip,omitempty"`
}

func (s ApplicationDocumentSelection) Overrides(frozen bool) ApplicationDocumentOverrides {
	return ApplicationDocumentOverrides{
		ResumeVersionID: strings.TrimSpace(s.ResumeVersionID),
		CoverVersionID:  strings.TrimSpace(s.CoverVersionID),
		ResumeUseSite:   s.ResumeUseSite,
		CoverSkip:       s.CoverSkip,
		Frozen:          frozen,
	}
}

// ReviewDocumentChoice is one selectable version in the review picker.
type ReviewDocumentChoice struct {
	VersionID     string `json:"version_id"`
	DocumentID    string `json:"document_id"`
	Kind          string `json:"kind"`
	Title         string `json:"title"`
	Source        string `json:"source,omitempty"`
	IsOriginal    bool   `json:"is_original,omitempty"`
	HasPDF        bool   `json:"has_pdf"`
	Reconstructible bool `json:"reconstructible,omitempty"`
}

// ReviewDocumentOptions lists eligible versions grouped by kind.
type ReviewDocumentOptions struct {
	Resume []ReviewDocumentChoice `json:"resume"`
	Cover  []ReviewDocumentChoice `json:"cover"`
}

// ReviewDocumentAction describes how one kind will be resolved.
type ReviewDocumentAction struct {
	Kind              string  `json:"kind"`
	Mode              string  `json:"mode,omitempty"`
	Action            string  `json:"action"` // policy | reuse | render | tailor | generate | site | skip | hold | unknown
	UsesAI            bool    `json:"uses_ai"`
	CreditsEstimate   *int64  `json:"credits_estimate,omitempty"` // nil when unknown; 0 for reuse/render
	HoldReason        string  `json:"hold_reason,omitempty"`
	VersionID         string  `json:"version_id,omitempty"`
}

// ReviewDocumentPreflight is shown before/after prepare in Review.
type ReviewDocumentPreflight struct {
	Policies           domain.DocumentPolicies      `json:"policies"`
	Fallbacks          domain.DocumentFallbackPolicy `json:"fallbacks"`
	Selection          ApplicationDocumentSelection `json:"selection"`
	Capabilities       FormDocumentCapabilities     `json:"capabilities"`
	CapabilitiesKnown  bool                         `json:"capabilities_known"`
	Prepared           bool                         `json:"prepared"`
	Resume             ReviewDocumentAction         `json:"resume"`
	Cover              ReviewDocumentAction         `json:"cover"`
	BlockingReasons    []string                     `json:"blocking_reasons,omitempty"`
	PackHoldReason     string                       `json:"pack_hold_reason,omitempty"`
}

var ErrReviewVersionForbidden = errors.New("document version not found")

// ValidateUserVersionKind ensures versionID belongs to userID and matches expected kind.
func (s *Service) ValidateUserVersionKind(ctx context.Context, userID, versionID, expectedKind string) error {
	versionID = strings.TrimSpace(versionID)
	if versionID == "" {
		return nil
	}
	if err := s.Store.ValidateVersionKind(ctx, userID, versionID, expectedKind); err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrReviewVersionForbidden
		}
		if errors.Is(err, ErrReviewVersionWrongKind) {
			return err
		}
		return ErrReviewVersionForbidden
	}
	return nil
}

// BuildReviewDocumentOptions flattens list response into picker choices.
func BuildReviewDocumentOptions(list ListResponse) ReviewDocumentOptions {
	out := ReviewDocumentOptions{
		Resume: []ReviewDocumentChoice{},
		Cover:  []ReviewDocumentChoice{},
	}
	for _, doc := range list.Documents {
		for _, v := range doc.Versions {
			ch := ReviewDocumentChoice{
				VersionID: v.ID, DocumentID: doc.ID, Kind: doc.Kind, Title: doc.Title,
				Source: v.Source, IsOriginal: doc.Kind == KindOriginalUpload || v.Source == SourceOriginalUpload,
				HasPDF: v.HasPDF, Reconstructible: v.Reconstructible,
			}
			switch doc.Kind {
			case KindResume, KindOriginalUpload:
				out.Resume = append(out.Resume, ch)
			case KindCoverLetter:
				out.Cover = append(out.Cover, ch)
			}
		}
	}
	return out
}

const (
	AITaskTailorResume  = "tailor_resume"
	AITaskGenerateCover = "generate_cover"
)

// ReviewCreditEstimator returns billing credits for an AI document task, or ok=false when unavailable.
type ReviewCreditEstimator func(task string) (credits int64, ok bool)

// DefaultAITokenEstimates match pre-call checks in the LLM client (chars/4 input, max_tokens output).
var DefaultAITokenEstimates = map[string][2]int{
	AITaskTailorResume:  {12000, 4096},
	AITaskGenerateCover: {8000, 2048},
}

func actionForResolved(r ResolvedDocument, capsKnown bool, est ReviewCreditEstimator) ReviewDocumentAction {
	a := ReviewDocumentAction{Kind: r.Kind, Mode: r.PolicyMode}
	switch {
	case r.Outcome == domain.DocumentOutcomeHoldReview:
		a.Action = "hold"
		a.HoldReason = r.HoldReason
	case r.UseSite || r.Outcome == domain.DocumentOutcomeSiteHosted:
		a.Action = "site"
		z := int64(0)
		a.CreditsEstimate = &z
	case r.Skip || r.Outcome == domain.DocumentOutcomeSkipped:
		a.Action = "skip"
		z := int64(0)
		a.CreditsEstimate = &z
	case r.NeedTailor:
		a.Action = "tailor"
		a.UsesAI = true
		if capsKnown && est != nil {
			if c, ok := est(AITaskTailorResume); ok {
				a.CreditsEstimate = &c
			}
		}
	case r.NeedGenerateCover:
		a.Action = "generate"
		a.UsesAI = true
		if capsKnown && est != nil {
			if c, ok := est(AITaskGenerateCover); ok {
				a.CreditsEstimate = &c
			}
		}
	case r.NeedDefaultUpload || r.VersionID != "":
		a.Action = "reuse"
		a.VersionID = r.VersionID
		z := int64(0)
		a.CreditsEstimate = &z
	default:
		a.Action = "unknown"
	}
	return a
}

// BuildReviewPreflight computes preflight for review UI.
func BuildReviewPreflight(
	policies domain.DocumentPolicies,
	selection ApplicationDocumentSelection,
	caps FormDocumentCapabilities,
	pack ApplicationDocumentPack,
	res ResolveResult,
	est ReviewCreditEstimator,
) ReviewDocumentPreflight {
	capsKnown := caps.Detected || pack.Prepared
	pf := ReviewDocumentPreflight{
		Policies:          policies,
		Fallbacks:         policies.Fallback,
		Selection:         selection,
		Capabilities:      caps,
		CapabilitiesKnown: capsKnown,
		Prepared:          pack.Prepared,
		Resume:            actionForResolved(res.Resume, capsKnown, est),
		Cover:             actionForResolved(res.Cover, capsKnown, est),
		PackHoldReason:    pack.HoldReason,
	}
	if !capsKnown {
		pf.BlockingReasons = append(pf.BlockingReasons, "Form document requirements unknown until you run Prepare documents")
	}
	if res.Hold {
		pf.BlockingReasons = append(pf.BlockingReasons, res.HoldReason)
	}
	if pack.Prepared {
		if pack.Resume.Outcome != "" {
			pf.Resume.Action = string(pack.Resume.Outcome)
			pf.Resume.VersionID = pack.Resume.ContentVersionID
			z := int64(0)
			pf.Resume.CreditsEstimate = &z
		}
		if pack.Cover.Outcome != "" {
			pf.Cover.Action = string(pack.Cover.Outcome)
			pf.Cover.VersionID = pack.Cover.ContentVersionID
			z := int64(0)
			pf.Cover.CreditsEstimate = &z
		}
	}
	return pf
}

// SelectionFromPack reads stored selection or legacy version columns.
func SelectionFromPack(pack ApplicationDocumentPack, resumeCol, coverCol string) ApplicationDocumentSelection {
	sel := pack.Selection
	if sel.ResumeVersionID == "" {
		sel.ResumeVersionID = resumeCol
	}
	if sel.CoverVersionID == "" {
		sel.CoverVersionID = coverCol
	}
	return sel
}

// PackAfterSelectionChange resets preparation after picker changes.
func PackAfterSelectionChange(sel ApplicationDocumentSelection) ApplicationDocumentPack {
	return ApplicationDocumentPack{
		Selection:  sel,
		HoldReason: "Document selection changed — prepare again to preview",
	}
}

// ResolveReviewInput is the data needed to preview review document actions.
type ResolveReviewInput struct {
	Policies                 domain.DocumentPolicies
	Defaults                 DefaultsView
	EffectiveResumeVersionID string
	Selection                ApplicationDocumentSelection
	Caps                     FormDocumentCapabilities
	Pack                     ApplicationDocumentPack
	HasConfirmedProfile      bool
	DefaultResumeExists      bool
	DefaultCoverExists       bool
	CreditEstimator          ReviewCreditEstimator
}

// ResolveReviewPreflight resolves policies + selection for the review UI.
func ResolveReviewPreflight(in ResolveReviewInput) (ReviewDocumentPreflight, ResolveResult) {
	var r Resolver
	res := r.Resolve(ResolveInput{
		Policies:                 in.Policies,
		Defaults:                 in.Defaults,
		EffectiveResumeVersionID: in.EffectiveResumeVersionID,
		Overrides:                in.Selection.Overrides(false),
		Caps:                     in.Caps,
		HasConfirmedProfile:      in.HasConfirmedProfile,
		DefaultResumeExists:      in.DefaultResumeExists,
		DefaultCoverExists:       in.DefaultCoverExists,
	})
	pf := BuildReviewPreflight(in.Policies, in.Selection, in.Caps, in.Pack, res, in.CreditEstimator)
	return pf, res
}
