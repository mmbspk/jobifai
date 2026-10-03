package documents_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/documents"
	"github.com/user/jobifai/internal/domain"
)

func baseResolveInput() documents.ResolveInput {
	return documents.ResolveInput{
		Policies: domain.DocumentPolicies{
			Version:            domain.DocumentPolicyMigrationVersion,
			ResumeMode:         domain.ResumeDocumentModeDefault,
			CoverMode:          domain.CoverDocumentModeWhenRequired,
			OnboardingComplete: true,
		},
		Defaults: documents.DefaultsView{
			ResumeVersionID:      "resume-v1",
			CoverLetterVersionID: "cover-v1",
		},
		HasConfirmedProfile: true,
		DefaultResumeExists: true,
		DefaultCoverExists:  true,
		Caps: documents.FormDocumentCapabilities{
			Detected:        true,
			ResumeFileSlots: 2,
			CoverOptional:   true,
		},
	}
}

func TestResolver_IndependentOverrides(t *testing.T) {
	t.Parallel()
	var r documents.Resolver
	in := baseResolveInput()
	in.Caps.CoverRequired = true
	in.Overrides = documents.ApplicationDocumentOverrides{ResumeVersionID: "override-resume"}
	out := r.Resolve(in)
	require.Equal(t, "override-resume", out.Resume.VersionID)
	require.Equal(t, domain.DocumentOutcomeLocalFile, out.Cover.Outcome)
	require.False(t, out.Hold)
}

func TestResolver_ResumeOverrideDoesNotSuppressCover(t *testing.T) {
	t.Parallel()
	var r documents.Resolver
	in := baseResolveInput()
	in.Policies.CoverMode = domain.CoverDocumentModeGeneralDefault
	in.Overrides = documents.ApplicationDocumentOverrides{ResumeVersionID: "only-resume"}
	out := r.Resolve(in)
	require.Equal(t, "only-resume", out.Resume.VersionID)
	require.Equal(t, "cover-v1", out.Cover.VersionID)
	require.True(t, out.Cover.NeedDefaultUpload)
}

func TestCoverIntent_WhenRequiredOnlyRequired(t *testing.T) {
	t.Parallel()
	var r documents.Resolver
	in := baseResolveInput()
	in.Policies.CoverMode = domain.CoverDocumentModeWhenRequired
	in.Caps = documents.FormDocumentCapabilities{Detected: true, CoverOptional: true, ResumeFileSlots: 1}
	out := r.Resolve(in)
	require.True(t, out.Cover.Skip)
}

func TestCoverIntent_WhenRequiredGeneratesForRequired(t *testing.T) {
	t.Parallel()
	var r documents.Resolver
	in := baseResolveInput()
	in.Policies.CoverMode = domain.CoverDocumentModeWhenRequired
	in.Caps = documents.FormDocumentCapabilities{Detected: true, CoverRequired: true}
	out := r.Resolve(in)
	require.True(t, out.Cover.NeedGenerateCover)
}

func TestCoverIntent_WhenAcceptedOptionalOnly(t *testing.T) {
	t.Parallel()
	var r documents.Resolver
	in := baseResolveInput()
	in.Policies.CoverMode = domain.CoverDocumentModeWhenAccepted
	in.Caps = documents.FormDocumentCapabilities{Detected: true, CoverOptional: true, ResumeFileSlots: 1}
	out := r.Resolve(in)
	require.True(t, out.Cover.NeedGenerateCover)
}

func TestResolve_UnknownCapsHoldsForDefaultResume(t *testing.T) {
	t.Parallel()
	var r documents.Resolver
	in := baseResolveInput()
	in.Caps = documents.FormDocumentCapabilities{Detected: false}
	out := r.Resolve(in)
	require.True(t, out.Hold)
}

func TestResolver_DefaultResumePlusTailoredCover(t *testing.T) {
	t.Parallel()
	var r documents.Resolver
	in := baseResolveInput()
	in.Policies.CoverMode = domain.CoverDocumentModeWhenRequired
	in.Caps.CoverRequired = true
	out := r.Resolve(in)
	require.Equal(t, "resume-v1", out.Resume.VersionID)
	require.True(t, out.Cover.NeedGenerateCover)
}

func TestResolver_MissingDefaultHolds(t *testing.T) {
	t.Parallel()
	var r documents.Resolver
	in := baseResolveInput()
	in.Defaults.ResumeVersionID = ""
	in.DefaultResumeExists = false
	out := r.Resolve(in)
	require.True(t, out.Hold)
	require.Contains(t, out.HoldReason, "default resume")
}

func TestResolver_SiteResumeAmbiguousHolds(t *testing.T) {
	t.Parallel()
	var r documents.Resolver
	in := baseResolveInput()
	in.Policies.ResumeMode = domain.ResumeDocumentModeSiteHosted
	in.Caps.SiteResumeAmbiguous = true
	out := r.Resolve(in)
	require.True(t, out.Hold)
	require.Contains(t, out.Resume.HoldReason, "ambiguous")
}

func TestResolver_SkipOptionalCoverWhenNotRequired(t *testing.T) {
	t.Parallel()
	var r documents.Resolver
	in := baseResolveInput()
	in.Policies.CoverMode = domain.CoverDocumentModeSkipOptional
	in.Caps.CoverRequired = false
	in.Caps.ResumeFileSlots = 1
	out := r.Resolve(in)
	require.True(t, out.Cover.Skip)
	require.False(t, out.Hold)
}
