package bot

import (
	"net/url"
	"strings"
)

// linkedInJobPath returns the canonical path segment that identifies the job
// within a LinkedIn URL (e.g. "/jobs/view/123456789"). Returns "" for non-job
// URLs or URLs that cannot be parsed. Used to detect same-job vs different-job
// URL drift during the apply flow.
func linkedInJobPath(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	p := strings.TrimRight(u.Path, "/")
	idx := strings.Index(strings.ToLower(p), "/jobs/view/")
	if idx < 0 {
		return ""
	}
	// Split at most into 5 parts: ["", "jobs", "view", "<id>", "rest..."]
	parts := strings.SplitN(p[idx:], "/", 5)
	if len(parts) < 4 || parts[3] == "" {
		return ""
	}
	return "/" + parts[1] + "/" + parts[2] + "/" + parts[3] // "/jobs/view/<id>"
}

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

// queueApplyOutcome maps a platformApply error from processApprovedQueue to the
// (outcome, stage, reasonCode) triple written to apply_attempts. It does NOT handle
// errAlreadyApplied — that sentinel is checked before this function is called.
//
// Errors from easyApply/seekApply already carry "easy apply:" or "seek apply:"
// prefixes, so skipReasonBelongsInTopMatches receives them directly without an
// additional label — previously the "approved queue: " prefix was prepended here
// which inadvertently prevented the blocked-ATS branch from ever matching.
func queueApplyOutcome(err error) (outcome AttemptOutcome, stage, reasonCode string) {
	if isSeekSubmitUnconfirmedError(err) {
		return OutcomeSubmissionUnknown, "confirmation", "seek_submit_unconfirmed"
	}
	if skipReasonBelongsInTopMatches(err.Error()) {
		return OutcomeBlocked, "apply", "not_automatable"
	}
	return classifyApplyOutcome(err), "apply", "apply_error"
}
