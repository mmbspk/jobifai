package domain

// Document policy migration version stored in general_settings.document_policies.version.
const DocumentPolicyMigrationVersion = 1

// Resume document selection policy (independent from cover letter).
type ResumeDocumentMode string

const (
	ResumeDocumentModeDefault    ResumeDocumentMode = "default" // explicit user default version
	ResumeDocumentModeTailorJob  ResumeDocumentMode = "tailor"  // LLM tailor per job from confirmed profile
	ResumeDocumentModeSiteHosted ResumeDocumentMode = "site"    // platform-hosted resume only
)

// Cover letter selection policy.
type CoverDocumentMode string

const (
	CoverDocumentModeWhenRequired   CoverDocumentMode = "when_required"
	CoverDocumentModeWhenAccepted   CoverDocumentMode = "when_accepted"
	CoverDocumentModeGeneralDefault CoverDocumentMode = "general_default"
	CoverDocumentModeSkipOptional   CoverDocumentMode = "skip_optional"
)

// DocumentFallbackPolicy records explicit user consent for fallbacks — never enabled by migration.
type DocumentFallbackPolicy struct {
	AllowSiteResumeWhenDefaultMissing bool `json:"allow_site_resume_when_default_missing,omitempty"`
	AllowGeneralCoverWhenGenerateFails bool `json:"allow_general_cover_when_generate_fails,omitempty"`
}

// DocumentPolicies groups independent resume/cover policies and migration metadata.
type DocumentPolicies struct {
	Version int `json:"version,omitempty"`

	ResumeMode ResumeDocumentMode `json:"resume_mode,omitempty"`
	CoverMode  CoverDocumentMode  `json:"cover_mode,omitempty"`

	Fallback DocumentFallbackPolicy `json:"fallback,omitempty"`

	// RegionalDefaults maps market name → default resume version id (optional explicit regional default).
	RegionalDefaults map[string]string `json:"regional_defaults,omitempty"`

	// OnboardingComplete is true after the user explicitly selected a default resume (new users).
	OnboardingComplete bool `json:"onboarding_complete,omitempty"`
}

// DocumentResolutionOutcome describes how a single document kind was resolved.
type DocumentResolutionOutcome string

const (
	DocumentOutcomeLocalFile   DocumentResolutionOutcome = "local_file"
	DocumentOutcomeSiteHosted  DocumentResolutionOutcome = "site_hosted"
	DocumentOutcomeSkipped     DocumentResolutionOutcome = "skipped"
	DocumentOutcomeHoldReview  DocumentResolutionOutcome = "hold_review"
)

// ApplicationDocumentRef is persisted on pending review / applied rows for provenance.
type ApplicationDocumentRef struct {
	Kind              string                    `json:"kind"` // resume | cover_letter
	Outcome           DocumentResolutionOutcome `json:"outcome"`
	ContentVersionID  string                    `json:"content_version_id,omitempty"`
	LocalPath         string                    `json:"local_path,omitempty"`
	RenderMarket      string                    `json:"render_market,omitempty"`
	RenderStyle       string                    `json:"render_style,omitempty"`
	RenderLanguage    string                    `json:"render_language,omitempty"`
	SiteHosted        bool                      `json:"site_hosted,omitempty"`
	HoldReason        string                    `json:"hold_reason,omitempty"`
	PolicyMode        string                    `json:"policy_mode,omitempty"`
}
