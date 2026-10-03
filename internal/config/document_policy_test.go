package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/domain"
)

func TestMigrateDocumentPolicies_LegacyFalseMapsToSiteResume(t *testing.T) {
	t.Parallel()
	gs := domain.GeneralSettings{GenerateNewResumeDocs: false}
	p := config.MigrateDocumentPolicies(gs)
	require.Equal(t, domain.ResumeDocumentModeSiteHosted, p.ResumeMode)
	require.Equal(t, domain.CoverDocumentModeWhenRequired, p.CoverMode)
	require.Equal(t, domain.DocumentPolicyMigrationVersion, p.Version)
	require.False(t, p.OnboardingComplete)
	require.False(t, p.Fallback.AllowSiteResumeWhenDefaultMissing)
}

func TestMigrateDocumentPolicies_LegacyTrueMapsToTailorAndCoverWhenAccepted(t *testing.T) {
	t.Parallel()
	gs := domain.GeneralSettings{GenerateNewResumeDocs: true}
	p := config.MigrateDocumentPolicies(gs)
	require.Equal(t, domain.ResumeDocumentModeTailorJob, p.ResumeMode)
	require.Equal(t, domain.CoverDocumentModeWhenAccepted, p.CoverMode)
}

func TestMigrateDocumentPolicies_Idempotent(t *testing.T) {
	t.Parallel()
	gs := domain.GeneralSettings{
		GenerateNewResumeDocs: true,
		DocumentPolicies: domain.DocumentPolicies{
			Version:    domain.DocumentPolicyMigrationVersion,
			ResumeMode: domain.ResumeDocumentModeDefault,
			CoverMode:  domain.CoverDocumentModeGeneralDefault,
		},
	}
	p := config.MigrateDocumentPolicies(gs)
	require.Equal(t, domain.ResumeDocumentModeDefault, p.ResumeMode)
	require.Equal(t, domain.CoverDocumentModeGeneralDefault, p.CoverMode)
}
