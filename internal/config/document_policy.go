package config

import "github.com/user/jobifai/internal/domain"

// RecommendedDocumentPolicies returns defaults for brand-new users (#59).
func RecommendedDocumentPolicies() domain.DocumentPolicies {
	return domain.DocumentPolicies{
		Version:            domain.DocumentPolicyMigrationVersion,
		ResumeMode:         domain.ResumeDocumentModeDefault,
		CoverMode:          domain.CoverDocumentModeWhenRequired,
		OnboardingComplete: false,
	}
}

// MigrateDocumentPolicies maps legacy generate_new_resume_docs into independent policies once.
// Idempotent: when version >= DocumentPolicyMigrationVersion, returns gs unchanged.
//
// Legacy cover behaviour (pre-#59): cover PDF was generated whenever the apply form exposed
// a second file input and generate_new_resume_docs was true — equivalent to "generate when
// the form accepts a cover upload" while tailoring was on. We map that to CoverModeWhenAccepted
// for migrated users who had generation enabled; site-resume users keep when_required so we
// still generate covers only when the form marks them required.
func MigrateDocumentPolicies(gs domain.GeneralSettings) domain.DocumentPolicies {
	p := gs.DocumentPolicies
	if p.Version >= domain.DocumentPolicyMigrationVersion && p.ResumeMode != "" && p.CoverMode != "" {
		return p
	}
	if gs.GenerateNewResumeDocs {
		p.ResumeMode = domain.ResumeDocumentModeTailorJob
		p.CoverMode = domain.CoverDocumentModeWhenAccepted
	} else {
		p.ResumeMode = domain.ResumeDocumentModeSiteHosted
		p.CoverMode = domain.CoverDocumentModeWhenRequired
	}
	p.Version = domain.DocumentPolicyMigrationVersion
	// Migration never sets onboarding_complete or enables fallbacks — explicit user action only.
	return p
}

// EnsureDocumentPolicies applies migration and fills empty modes for partially stored rows.
func EnsureDocumentPolicies(gs *domain.GeneralSettings) {
	migrated := MigrateDocumentPolicies(*gs)
	gs.DocumentPolicies = migrated
}
