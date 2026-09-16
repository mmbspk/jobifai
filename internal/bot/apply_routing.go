package bot

import "strings"

// skipReasonIsApplyFailure reports whether a skip reason came from an Easy/Quick Apply attempt.
func skipReasonIsApplyFailure(reason string) bool {
	lower := strings.ToLower(reason)
	return strings.HasPrefix(lower, "seek apply:") ||
		strings.HasPrefix(lower, "easy apply:") ||
		strings.HasPrefix(lower, "quick apply:")
}

// skipReasonBelongsInTopMatches reports apply-failure reasons that are really manual-apply
// jobs (external ATS, bot protection, mis-detected Easy Apply), not Quick Apply automation
// failures that belong in Cannot Apply.
func skipReasonBelongsInTopMatches(reason string) bool {
	if !skipReasonIsApplyFailure(reason) {
		return false
	}
	lower := strings.ToLower(reason)
	for _, marker := range []string{
		"blocked", "smartrecruiters", "external ats", "external site", "external application",
		"datadome", "captcha", "not automatable",
		"only easy/quick apply is supported",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
