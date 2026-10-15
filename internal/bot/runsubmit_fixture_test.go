package bot

// Browser fixture test for the runSubmit approval path.
//
// Tests that the post-apply identity check added to runSubmit prevents a
// wrong-job Applied record when the page URL drifts between the final form
// submission and the jobs_applied write. Uses a real Chromium browser (skipped
// when Chromium is unavailable) and a local httptest server that serves HTML
// fixtures at LinkedIn-shaped URL paths so verifyJobIdentity fires correctly.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/browser"
	"github.com/user/jobifai/internal/config"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
)

// TestRunSubmit_PostApplyDriftNoAppliedRecord proves that when the review-approval
// path calls easyApply and it succeeds (submit button clicked, success page shown),
// but the final page URL is at a different job than the declared one, runSubmit
// detects the post-apply identity drift and:
//   - returns an error
//   - writes no jobs_applied record
//   - leaves jobs_pending_review intact for manual reconciliation
//   - records OutcomeSubmissionUnknown in apply_attempts
//
// This covers the fourth submission caller identified in PR #97 review round 5.
//
// Fixture layout:
//
//	/jobs/view/123456789       — correct job, CTA navigates to modal on same path
//	/jobs/view/123456789/modal — modal with a submit button that navigates to wrong job
//	/jobs/view/999888          — success page ("application submitted") at wrong job URL
func TestRunSubmit_PostApplyDriftNoAppliedRecord(t *testing.T) {
	// Reuse the fixture's Chrome through the supported remote-debug setting.
	// This exercises runSubmit without a second launcher requiring a Linux sandbox.
	_, endpoint := launchTestBrowserEndpoint(t)
	parsedEndpoint, err := url.Parse(endpoint)
	require.NoError(t, err)
	port, err := strconv.Atoi(parsedEndpoint.Port())
	require.NoError(t, err)

	mux := http.NewServeMux()
	mux.HandleFunc("/jobs/view/123456789", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html><html><body>
<main data-job-id="123456789">
  <h1>Test Role at TestCo</h1>
  <button data-live-test-easy-apply
          onclick="window.location.href='/jobs/view/123456789/modal'">Easy Apply</button>
</main>
</body></html>`)
	})
	mux.HandleFunc("/jobs/view/123456789/modal", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html><html><body>
<main data-job-id="123456789"><h1>Test Role at TestCo</h1>
<div role="dialog" aria-label="Easy Apply">
  <button data-live-test-easy-apply-submit-button
          onclick="window.location.href='/jobs/view/999888'">Submit Application</button>
</div></main>
</body></html>`)
	})
	mux.HandleFunc("/jobs/view/999888", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html><html><body>
<main data-job-id="123456789"><p>Application submitted. Your application was sent to TestCo.</p></main>
</body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	cfgStore := config.NewStore(db)
	secrets := config.NewSecretsStore(db, "runsubmit-drift-test-key")
	sessions := browser.NewSessionStore(db, secrets)

	const userID = "rs-drift-user"
	require.NoError(t, cfgStore.Set(domain.SystemUserID, config.KeyGeneralSettings, domain.GeneralSettings{
		Browser:       domain.BrowserConfig{RemoteDebugPort: port},
		HumanBehavior: domain.HumanBehaviorConfig{InteractionPauseMin: 0.01, InteractionPauseMax: 0.02},
	}))
	resolved := config.ResolveOperationalSettings(cfgStore, userID, false)
	require.Equal(t, port, resolved.Browser.RemoteDebugPort, "fixture must use the existing Chrome instead of launching another process")
	require.NoError(t, cfgStore.Set(userID, "resume_profile", domain.ResumeProfile{
		Summary: "Test profile.",
		PersonalInformation: domain.PersonalInformation{
			Name:  "Test User",
			Email: "test@example.com",
		},
	}))
	raw, err := browser.MarshalCookies([]browser.Cookie{
		{Name: "li_at", Value: "fake-token", Domain: ".linkedin.com", Path: "/"},
	})
	require.NoError(t, err)
	require.NoError(t, sessions.Save(userID, string(domain.PlatformLinkedIn), string(domain.LoginMethodManual), raw))

	// Seed jobs_pending_review so runSubmit can look up the score and halal verdict.
	const jobID = "123456789"
	jobURL := srv.URL + "/jobs/view/" + jobID
	_, err = db.Exec(
		`INSERT OR IGNORE INTO jobs_pending_review
		 (job_id, user_id, company, role, location, platform, link, suitability_score, easy_apply, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, datetime('now'))`,
		jobID, userID, "TestCo", "Test Role", "Remote", string(domain.PlatformLinkedIn), jobURL, 8,
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	m := NewManager(ctx, db, cfgStore, secrets, sessions, nil, nil, nil, nil, "")
	err = m.runSubmit(ctx, userID, SubmitRequest{
		JobID:    jobID,
		Link:     jobURL,
		Platform: string(domain.PlatformLinkedIn),
		Role:     "Test Role",
		Company:  "TestCo",
		Location: "Remote",
	})
	require.Error(t, err, "runSubmit must return error when post-apply drift is detected")
	assert.Contains(t, err.Error(), "submission could not be confirmed",
		"error must identify this as a post-apply identity failure")

	// No jobs_applied row — post-apply drift must not produce an Applied record.
	var appliedCount int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM jobs_applied WHERE link = ? AND user_id = ?`,
		jobURL, userID,
	).Scan(&appliedCount))
	assert.Equal(t, 0, appliedCount, "jobs_applied must be empty after post-apply drift in runSubmit")

	// jobs_pending_review must still exist — preserved for reconciliation.
	var pendingCount int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM jobs_pending_review WHERE job_id = ? AND user_id = ?`,
		jobID, userID,
	).Scan(&pendingCount))
	assert.Equal(t, 1, pendingCount, "jobs_pending_review must be preserved after post-apply drift")

	// apply_attempts must record OutcomeSubmissionUnknown.
	var outcome string
	require.NoError(t, db.QueryRow(
		`SELECT outcome FROM apply_attempts WHERE url = ? AND user_id = ?`,
		jobURL, userID,
	).Scan(&outcome))
	assert.Equal(t, string(OutcomeSubmissionUnknown), outcome,
		"attempt outcome must be submission_unknown after post-apply drift in runSubmit")
}
