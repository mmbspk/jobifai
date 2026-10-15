package bot

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/go-rod/rod"
)

// verifyJobIdentity checks that the page's current URL carries the expected LinkedIn
// job path. Returns nil when expectedJobPath is empty (non-LinkedIn URL or no declared
// intent) or when the URL confirms the same job. Returns an error — and the caller must
// abort — when:
//   - the page URL is unreadable
//   - the URL carries no LinkedIn job path
//   - the URL carries a different job ID
//
// Callers check this before form actions and before accepting confirmation.
// Losing identity after submission means submission_unknown, never success.
func verifyJobIdentity(page *rod.Page, expectedJobPath string) error {
	if expectedJobPath == "" {
		return nil
	}
	info, err := page.Timeout(5 * time.Second).Info()
	if err != nil || info == nil || info.URL == "" {
		return fmt.Errorf("%w: job identity: cannot read page URL (expected job path %s)", errLinkedInIdentity, expectedJobPath)
	}
	u, parseErr := url.Parse(info.URL)
	if parseErr != nil || !(u.Hostname() == "linkedin.com" || strings.HasSuffix(u.Hostname(), ".linkedin.com") || u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1") {
		return fmt.Errorf("%w: unexpected page origin", errLinkedInIdentity)
	}
	cp := linkedInJobPath(info.URL)
	if cp == "" {
		// Collections are acceptable only when both selected ID and rendered pane
		// are verified by the same resolver. referenceJobId is never evidence.
		r, resolveErr := resolveLinkedInJob(page, strings.TrimPrefix(expectedJobPath, "/jobs/view/"), false)
		if resolveErr == nil && r.IdentityVerified {
			return nil
		}
		return fmt.Errorf("%w: job identity: page at %s has no LinkedIn job path (expected %s)", errLinkedInIdentity, info.URL, expectedJobPath)
	}
	if cp != expectedJobPath {
		return fmt.Errorf("%w: job identity drift: page at %s but declared job is %s", errLinkedInIdentity, info.URL, expectedJobPath)
	}
	r, resolveErr := resolveLinkedInJob(page, strings.TrimPrefix(expectedJobPath, "/jobs/view/"), false)
	if resolveErr != nil {
		return resolveErr
	}
	if !r.IdentityVerified {
		return r.resolutionError()
	}
	return nil
}

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
	if linkedInNeedsReview(err) {
		return linkedInReviewOutcome(err)
	}
	if isSeekSubmitUnconfirmedError(err) {
		return OutcomeSubmissionUnknown, "confirmation", "seek_submit_unconfirmed"
	}
	if skipReasonBelongsInTopMatches(err.Error()) {
		return OutcomeBlocked, "apply", "not_automatable"
	}
	return classifyApplyOutcome(err), "apply", "apply_error"
}
