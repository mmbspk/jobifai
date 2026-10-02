package config

import "github.com/user/jobifai/internal/domain"

// DefaultUserApplicationSettings are seeded for new trial users and used to back-fill unset stored fields.
func DefaultUserApplicationSettings() domain.GeneralSettings {
	return domain.GeneralSettings{
		RequireReview:       true,
		JobSuitabilityScore: 7,
		MaxJobsPerKeyword:   25,
		HumanBehavior: domain.HumanBehaviorConfig{
			DailyApplicationLimit: domain.TrialDailyApplicationLimit,
		},
	}
}

// ApplyUnsetUserApplicationFields fills zero-valued application prefs from defaults without
// overwriting choices the user saved (e.g. review off with a custom suitability threshold).
func ApplyUnsetUserApplicationFields(stored domain.GeneralSettings) domain.GeneralSettings {
	def := DefaultUserApplicationSettings()
	out := stored
	unsetPrefs := out.JobSuitabilityScore == 0 && out.MaxJobsPerKeyword == 0 &&
		!out.HalalJobFilter && !out.GenerateNewResumeDocs && out.DefaultResumeMarket == ""
	if out.JobSuitabilityScore == 0 {
		out.JobSuitabilityScore = def.JobSuitabilityScore
	}
	if out.MaxJobsPerKeyword == 0 {
		out.MaxJobsPerKeyword = def.MaxJobsPerKeyword
	}
	if unsetPrefs && out.HumanBehavior.DailyApplicationLimit == 0 {
		out.HumanBehavior.DailyApplicationLimit = def.HumanBehavior.DailyApplicationLimit
	}
	if unsetPrefs && !stored.RequireReview {
		out.RequireReview = def.RequireReview
	}
	return out
}
