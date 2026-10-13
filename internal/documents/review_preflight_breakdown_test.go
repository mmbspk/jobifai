package documents_test

// Gap 7 tests: explicit preflight credit breakdown for every action type.
//
// BuildReviewPreflight is called directly so we can control ResolveResult fields precisely
// without going through the full policy resolver.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/documents"
	"github.com/user/jobifai/internal/domain"
)

// fixedEstimator returns a deterministic per-task credit estimate for testing.
// tailor resume → 100 credits; generate cover → 60 credits.
func fixedEstimator(task string) (int64, bool) {
	switch task {
	case documents.AITaskTailorResume:
		return 100, true
	case documents.AITaskGenerateCover:
		return 60, true
	}
	return 0, false
}

func buildPreflightForAction(res documents.ResolveResult, renderCredits int64) documents.ReviewDocumentPreflight {
	return documents.BuildReviewPreflight(
		domain.DocumentPolicies{
			ResumeMode:         domain.ResumeDocumentModeDefault,
			CoverMode:          domain.CoverDocumentModeSkipOptional,
			OnboardingComplete: true,
		},
		documents.ApplicationDocumentSelection{},
		documents.FormDocumentCapabilities{Detected: true, ResumeFileSlots: 1, CoverOptional: true},
		documents.ApplicationDocumentPack{},
		res,
		fixedEstimator,
		renderCredits,
	)
}

// TestPreflight_TailorResume_BreakdownIsAIPlusRender verifies that when the resolver
// requires tailoring, CreditsEstimate = AI + renderCredits and CreditBreakdown reflects
// the correct split.
func TestPreflight_TailorResume_BreakdownIsAIPlusRender(t *testing.T) {
	t.Parallel()
	const renderCredits = int64(3)
	res := documents.ResolveResult{
		Resume: documents.ResolvedDocument{Kind: documents.KindResume, NeedTailor: true},
		Cover:  documents.ResolvedDocument{Kind: documents.KindCoverLetter, Skip: true},
	}
	pf := buildPreflightForAction(res, renderCredits)

	resumeAction := pf.Resume
	assert.Equal(t, "tailor", resumeAction.Action, "tailor path must produce action=tailor")
	assert.True(t, resumeAction.UsesAI, "tailor uses AI")
	require.NotNil(t, resumeAction.CreditsEstimate, "credits estimate must be set for tailor")
	assert.Equal(t, int64(100+3), *resumeAction.CreditsEstimate, "total = AI + renderCredits")
	require.NotNil(t, resumeAction.CreditBreakdown, "breakdown must be set when estimate is set")
	assert.Equal(t, int64(100), resumeAction.CreditBreakdown.AI, "AI component must match estimator")
	assert.Equal(t, int64(3), resumeAction.CreditBreakdown.Compute, "Compute component must match renderCredits")
}

// TestPreflight_GenerateCover_BreakdownIsAIPlusRender verifies that generating a cover letter
// carries the same AI + Compute breakdown structure as resume tailoring.
func TestPreflight_GenerateCover_BreakdownIsAIPlusRender(t *testing.T) {
	t.Parallel()
	const renderCredits = int64(3)
	res := documents.ResolveResult{
		Resume: documents.ResolvedDocument{Kind: documents.KindResume, VersionID: "rv1", NeedDefaultUpload: true},
		Cover:  documents.ResolvedDocument{Kind: documents.KindCoverLetter, NeedGenerateCover: true},
	}
	pf := buildPreflightForAction(res, renderCredits)

	coverAction := pf.Cover
	assert.Equal(t, "generate", coverAction.Action, "cover generation must produce action=generate")
	assert.True(t, coverAction.UsesAI, "cover generation uses AI")
	require.NotNil(t, coverAction.CreditsEstimate)
	assert.Equal(t, int64(60+3), *coverAction.CreditsEstimate, "total = AI + renderCredits")
	require.NotNil(t, coverAction.CreditBreakdown)
	assert.Equal(t, int64(60), coverAction.CreditBreakdown.AI)
	assert.Equal(t, int64(3), coverAction.CreditBreakdown.Compute)
}

// TestPreflight_Reuse_ZeroCredits verifies that reusing an existing version has zero cost.
func TestPreflight_Reuse_ZeroCredits(t *testing.T) {
	t.Parallel()
	const renderCredits = int64(3)
	res := documents.ResolveResult{
		Resume: documents.ResolvedDocument{Kind: documents.KindResume, VersionID: "rv1", NeedDefaultUpload: true},
		Cover:  documents.ResolvedDocument{Kind: documents.KindCoverLetter, Skip: true},
	}
	pf := buildPreflightForAction(res, renderCredits)

	resumeAction := pf.Resume
	assert.Equal(t, "reuse", resumeAction.Action)
	assert.False(t, resumeAction.UsesAI, "reuse must not require AI")
	require.NotNil(t, resumeAction.CreditsEstimate)
	assert.Equal(t, int64(0), *resumeAction.CreditsEstimate, "reuse is zero cost")
	require.NotNil(t, resumeAction.CreditBreakdown)
	assert.Equal(t, int64(0), resumeAction.CreditBreakdown.AI)
	assert.Equal(t, int64(0), resumeAction.CreditBreakdown.Compute)
}

// TestPreflight_SiteResume_ZeroCredits verifies that using the job site's hosted resume is free.
func TestPreflight_SiteResume_ZeroCredits(t *testing.T) {
	t.Parallel()
	const renderCredits = int64(3)
	res := documents.ResolveResult{
		Resume: documents.ResolvedDocument{Kind: documents.KindResume, UseSite: true},
		Cover:  documents.ResolvedDocument{Kind: documents.KindCoverLetter, Skip: true},
	}
	pf := buildPreflightForAction(res, renderCredits)

	resumeAction := pf.Resume
	assert.Equal(t, "site", resumeAction.Action, "site path must produce action=site")
	assert.False(t, resumeAction.UsesAI)
	require.NotNil(t, resumeAction.CreditsEstimate)
	assert.Equal(t, int64(0), *resumeAction.CreditsEstimate, "site usage is zero cost")
	require.NotNil(t, resumeAction.CreditBreakdown)
	assert.Equal(t, int64(0), resumeAction.CreditBreakdown.AI)
	assert.Equal(t, int64(0), resumeAction.CreditBreakdown.Compute)
}

// TestPreflight_SkipCover_ZeroCredits verifies that skipping cover is zero cost.
func TestPreflight_SkipCover_ZeroCredits(t *testing.T) {
	t.Parallel()
	const renderCredits = int64(3)
	res := documents.ResolveResult{
		Resume: documents.ResolvedDocument{Kind: documents.KindResume, VersionID: "rv1", NeedDefaultUpload: true},
		Cover:  documents.ResolvedDocument{Kind: documents.KindCoverLetter, Skip: true},
	}
	pf := buildPreflightForAction(res, renderCredits)

	coverAction := pf.Cover
	assert.Equal(t, "skip", coverAction.Action, "skip path must produce action=skip")
	assert.False(t, coverAction.UsesAI)
	require.NotNil(t, coverAction.CreditsEstimate)
	assert.Equal(t, int64(0), *coverAction.CreditsEstimate, "skip is zero cost")
	require.NotNil(t, coverAction.CreditBreakdown)
	assert.Equal(t, int64(0), coverAction.CreditBreakdown.AI)
	assert.Equal(t, int64(0), coverAction.CreditBreakdown.Compute)
}

// TestPreflight_RenderCreditsZero_TailorOnlyAICost verifies that when metering is disabled
// (renderCredits=0), the total for a tailor action equals only the AI cost.
func TestPreflight_RenderCreditsZero_TailorOnlyAICost(t *testing.T) {
	t.Parallel()
	const renderCredits = int64(0) // metering disabled
	res := documents.ResolveResult{
		Resume: documents.ResolvedDocument{Kind: documents.KindResume, NeedTailor: true},
		Cover:  documents.ResolvedDocument{Kind: documents.KindCoverLetter, Skip: true},
	}
	pf := buildPreflightForAction(res, renderCredits)

	resumeAction := pf.Resume
	require.NotNil(t, resumeAction.CreditsEstimate)
	assert.Equal(t, int64(100), *resumeAction.CreditsEstimate, "no render credits → total = AI only")
	require.NotNil(t, resumeAction.CreditBreakdown)
	assert.Equal(t, int64(100), resumeAction.CreditBreakdown.AI)
	assert.Equal(t, int64(0), resumeAction.CreditBreakdown.Compute)
}
