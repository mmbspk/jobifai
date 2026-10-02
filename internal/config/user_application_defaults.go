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

// isLegacySparseApplicationRecord detects pre-fix trial rows that only stored a daily cap.
func isLegacySparseApplicationRecord(stored domain.GeneralSettings) bool {
	if stored.JobSuitabilityScore != 0 || stored.MaxJobsPerKeyword != 0 {
		return false
	}
	if stored.RequireReview || stored.HalalJobFilter || stored.GenerateNewResumeDocs {
		return false
	}
	if stored.DefaultResumeMarket != "" {
		return false
	}
	return stored.HumanBehavior.DailyApplicationLimit > 0
}

// ApplyUnsetUserApplicationFields upgrades legacy sparse trial rows to full application defaults.
func ApplyUnsetUserApplicationFields(stored domain.GeneralSettings) domain.GeneralSettings {
	if !isLegacySparseApplicationRecord(stored) {
		return stored
	}
	def := DefaultUserApplicationSettings()
	out := stored
	out.JobSuitabilityScore = def.JobSuitabilityScore
	out.MaxJobsPerKeyword = def.MaxJobsPerKeyword
	out.RequireReview = def.RequireReview
	if out.HumanBehavior.DailyApplicationLimit == 0 {
		out.HumanBehavior.DailyApplicationLimit = def.HumanBehavior.DailyApplicationLimit
	}
	return out
}
