package bot

// Browser fixture tests for the LinkedIn Easy Apply detection and button-click
// guards. These tests use a real Chromium browser via go-rod and a local
// httptest server that serves HTML fixtures at a LinkedIn-shaped URL path
// (/jobs/view/<id>) so window.location.pathname matches the real LinkedIn URL
// format.
//
// The tests are skipped automatically when no Chromium binary is available
// (CI without a display or without Chromium installed). Run locally with:
//
//	go test ./internal/bot/... -run TestDetect -v
//
// Each fixture intentionally reflects a real scenario from the reported bug:
// LinkedIn embeds hidden <a> elements with opensduiapplyflow in their href on
// 3rd-party (non-Easy-Apply) job pages. Before the fix, these caused false
// detection and wrong-job navigation.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llm"
)

// launchTestBrowser starts a headless Chromium for the test and returns a
// connected *rod.Browser. Skips the test if Chromium cannot be launched.
func launchTestBrowser(t *testing.T) *rod.Browser {
	t.Helper()
	l := launcher.New().Headless(true).NoSandbox(os.Getenv("CI") != "")
	if bin := os.Getenv("ROD_BROWSER_BIN"); bin != "" {
		l = l.Bin(bin)
	}
	u, err := l.Launch()
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("Chromium required in CI: %v", err)
		}
		t.Skipf("skip browser fixture test: cannot launch Chromium: %v", err)
	}
	br := rod.New().ControlURL(u).MustConnect()
	t.Cleanup(func() { _ = br.Close() })
	return br
}

// fixtureServer starts an httptest server serving html at /jobs/view/targetJobID
// and the "wrong" job at /jobs/view/wrongJobID. Returns server + cleanup.
func fixtureServer(t *testing.T, targetJobID, wrongJobID, targetHTML, wrongHTML string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/jobs/view/"+targetJobID, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, targetHTML)
	})
	if wrongJobID != "" {
		mux.HandleFunc("/jobs/view/"+wrongJobID, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, wrongHTML)
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// navigateAndWait opens a page and waits for the DOM to stabilize.
func navigateAndWait(t *testing.T, br *rod.Browser, url string) *rod.Page {
	t.Helper()
	page := br.MustPage("")
	t.Cleanup(func() { _ = page.Close() })
	require.NoError(t, page.Navigate(url))
	require.NoError(t, page.Timeout(10*time.Second).WaitLoad())
	return page
}

// ── Fixture HTML ──────────────────────────────────────────────────────────────

// hiddenDisplayNoneHTML: 3rd-party job page with a display:none preload link.
// detectLinkedInEasyApply must return false; jsLinkedInClickCTA must return "".
const hiddenDisplayNoneHTML = `<!DOCTYPE html><html><body>
<main data-job-id="123456789">
  <h1>Project Manager at ExternalCo</h1>
  <!-- 3rd-party apply — the visible CTA goes to an external ATS -->
  <a href="https://external-ats.com/apply" aria-label="Apply on company website">Apply on company website</a>
  <!-- LinkedIn SDUI preload link — hidden via display:none, must be ignored -->
  <a href="/jobs/view/123456789?openSDUIApplyFlow=true" aria-label="Easy Apply"
     style="display:none">Easy Apply</a>
</main>
</body></html>`

// hiddenVisibilityHiddenHTML: preload link hidden via visibility:hidden (nonzero dimensions).
// Dimension-only check would pass (rect.width > 0); full visibility check must reject it.
const hiddenVisibilityHiddenHTML = `<!DOCTYPE html><html><body>
<main data-job-id="123456789">
  <h1>Marketing Lead at ExternalCo</h1>
  <a href="https://external-ats.com/apply">Apply on company website</a>
  <!-- visibility:hidden link — has nonzero dimensions but must be treated as hidden -->
  <a href="/jobs/view/123456789?openSDUIApplyFlow=true" aria-label="Easy Apply"
     style="visibility:hidden; width:100px; height:40px; display:block">Easy Apply</a>
</main>
</body></html>`

// differentJobLinkHTML: a visible opensduiapplyflow link that targets a DIFFERENT job ID.
// Page is served at /jobs/view/123456789; the link points to /jobs/view/999888.
// The link has no "Easy Apply" label so it is only matched via the href check (check 2).
// Both detectLinkedInEasyApply and jsLinkedInClickCTA must reject this link.
const differentJobLinkHTML = `<!DOCTYPE html><html><body>
<main data-job-id="123456789">
  <h1>Software Engineer at Acme — 3rd party apply</h1>
  <!-- Visible link with opensduiapplyflow but targets a different job.
       No "Easy Apply" aria-label so it is only a candidate via the href check.
       The job ID mismatch (999888 vs current page 123456789) must cause rejection. -->
  <a href="/jobs/view/999888?openSDUIApplyFlow=true"
     style="display:block; width:120px; height:40px">Apply Now</a>
</main>
</body></html>`

// differentJobLabeledHTML: a visible link labelled "Easy Apply" that targets a
// DIFFERENT job ID. This is the exact gap from reviewer gap #1 — the label
// match must not fire before the href destination is validated.
const differentJobLabeledHTML = `<!DOCTYPE html><html><body>
<main data-job-id="123456789">
  <h1>Accountant at ExternalCo — 3rd party apply</h1>
  <!-- Visible, "Easy Apply"-labelled link whose href targets a different job.
       Before the fix, check 1 accepted this via the label match before
       checking the href destination. Both detect and click must reject it. -->
  <a href="/jobs/view/999888?openSDUIApplyFlow=true"
     aria-label="Easy Apply"
     style="display:block; width:120px; height:40px">Easy Apply</a>
</main>
</body></html>`

// sameJobVisibleCTAHTML: a visible Easy Apply button for the same job.
// detectLinkedInEasyApply must return true; jsLinkedInClickCTA must return "clicked".
const sameJobVisibleCTAHTML = `<!DOCTYPE html><html><body>
<main data-job-id="123456789">
  <h1>Data Analyst at ValidCo</h1>
  <!-- Visible Easy Apply CTA for the correct job — must be detected and clicked -->
  <button aria-label="Easy Apply" style="display:inline-block; width:120px; height:40px">
    Easy Apply
  </button>
</main>
</body></html>`

// sameJobSDUILinkHTML: a visible opensduiapplyflow link targeting the SAME job.
// detectLinkedInEasyApply must return true; jsLinkedInClickCTA must return "clicked".
const sameJobSDUILinkHTML = `<!DOCTYPE html><html><body>
<main data-job-id="123456789">
  <h1>Business Analyst at ValidCo</h1>
  <!-- Visible SDUI link targeting the same job — must be accepted -->
  <a href="/jobs/view/123456789?openSDUIApplyFlow=true" aria-label="Easy Apply"
     style="display:inline-block; width:120px; height:40px">Easy Apply</a>
</main>
</body></html>`

// wrongJobPageHTML: a simple page at /jobs/view/999888, used to simulate URL drift.
const wrongJobPageHTML = `<!DOCTYPE html><html><body>
<main data-job-id="123456789"><h1>Wrong Job Page</h1></main>
</body></html>`

// ── Tests ─────────────────────────────────────────────────────────────────────

// TestDetect_HiddenDisplayNone verifies that a display:none preload link is
// not treated as an Easy Apply CTA and jsLinkedInClickCTA does not click it.
func TestDetect_HiddenDisplayNone(t *testing.T) {
	br := launchTestBrowser(t)
	srv := fixtureServer(t, "123456789", "", hiddenDisplayNoneHTML, "")
	page := navigateAndWait(t, br, srv.URL+"/jobs/view/123456789")

	assert.False(t, detectLinkedInEasyApply(page), "display:none link must not be detected as Easy Apply")
	res, err := page.Eval(jsLinkedInClickCTA)
	require.NoError(t, err)
	assert.Equal(t, "", res.Value.String(), "jsLinkedInClickCTA must not click a display:none link")
}

// TestDetect_HiddenVisibilityHidden verifies that a visibility:hidden preload
// link (which has nonzero dimensions) is not treated as an Easy Apply CTA.
// This covers the reviewer's gap: dimension-only checks miss visibility:hidden.
func TestDetect_HiddenVisibilityHidden(t *testing.T) {
	br := launchTestBrowser(t)
	srv := fixtureServer(t, "123456789", "", hiddenVisibilityHiddenHTML, "")
	page := navigateAndWait(t, br, srv.URL+"/jobs/view/123456789")

	assert.False(t, detectLinkedInEasyApply(page), "visibility:hidden link must not be detected as Easy Apply")
	res, err := page.Eval(jsLinkedInClickCTA)
	require.NoError(t, err)
	assert.Equal(t, "", res.Value.String(), "jsLinkedInClickCTA must not click a visibility:hidden link")
}

// TestDetect_DifferentJobLinkRejected verifies that a visible link whose href
// carries a different job ID than the current page is rejected by both the
// detector and jsLinkedInClickCTA. Covers the opensduiapplyflow-only path (check 2).
func TestDetect_DifferentJobLinkRejected(t *testing.T) {
	br := launchTestBrowser(t)
	// Page is at /jobs/view/123456789; the link targets /jobs/view/999888.
	srv := fixtureServer(t, "123456789", "", differentJobLinkHTML, "")
	page := navigateAndWait(t, br, srv.URL+"/jobs/view/123456789")

	assert.False(t, detectLinkedInEasyApply(page), "link to different job must not be detected as Easy Apply")
	res, err := page.Eval(jsLinkedInClickCTA)
	require.NoError(t, err)
	assert.Equal(t, "", res.Value.String(), "jsLinkedInClickCTA must not click a link that targets a different job")
}

// TestDetect_DifferentJobLabeledLinkRejected verifies that a visible link
// labelled "Easy Apply" that targets a DIFFERENT job ID is also rejected.
// This covers reviewer gap #1: check 1 (label match) must validate the href
// destination before accepting any candidate.
func TestDetect_DifferentJobLabeledLinkRejected(t *testing.T) {
	br := launchTestBrowser(t)
	srv := fixtureServer(t, "123456789", "", differentJobLabeledHTML, "")
	page := navigateAndWait(t, br, srv.URL+"/jobs/view/123456789")

	assert.False(t, detectLinkedInEasyApply(page), "labelled Easy Apply link to different job must not be detected")
	res, err := page.Eval(jsLinkedInClickCTA)
	require.NoError(t, err)
	assert.Equal(t, "", res.Value.String(), "jsLinkedInClickCTA must not click a labelled link to a different job")
}

// TestDetect_ValidButtonClicked verifies that a visible, same-job Easy Apply
// button IS detected and clicked. Regression guard: the fixes must not prevent
// legitimate CTAs from working.
func TestDetect_ValidButtonClicked(t *testing.T) {
	br := launchTestBrowser(t)
	srv := fixtureServer(t, "123456789", "", sameJobVisibleCTAHTML, "")
	page := navigateAndWait(t, br, srv.URL+"/jobs/view/123456789")

	assert.True(t, detectLinkedInEasyApply(page), "visible Easy Apply button must be detected")
	res, err := page.Eval(jsLinkedInClickCTA)
	require.NoError(t, err)
	assert.Equal(t, "clicked", res.Value.String(), "jsLinkedInClickCTA must click a visible Easy Apply button")
}

// TestDetect_SameJobSDUILinkClicked verifies that a visible opensduiapplyflow
// link targeting the SAME job is accepted and clicked by jsLinkedInClickCTA.
func TestDetect_SameJobSDUILinkClicked(t *testing.T) {
	br := launchTestBrowser(t)
	srv := fixtureServer(t, "123456789", "", sameJobSDUILinkHTML, "")
	page := navigateAndWait(t, br, srv.URL+"/jobs/view/123456789")

	assert.True(t, detectLinkedInEasyApply(page), "visible SDUI link for same job must be detected")
	res, err := page.Eval(jsLinkedInClickCTA)
	require.NoError(t, err)
	assert.Equal(t, "clicked", res.Value.String(), "jsLinkedInClickCTA must click a visible SDUI link for the same job")
}

// ── Identity drift — entry check ─────────────────────────────────────────────

// TestEasyApply_AbortsOnIdentityDrift verifies that easyApply returns an
// identity error when the loaded page is at a different job URL than declared.
// This is the entry check: it fires before jsPoll, before reviewPrepareOnly,
// and before any form interaction.
func TestEasyApply_AbortsOnIdentityDrift(t *testing.T) {
	br := launchTestBrowser(t)
	srv := fixtureServer(t, "999888", "", wrongJobPageHTML, "")

	// Navigate to the WRONG job page.
	page := navigateAndWait(t, br, srv.URL+"/jobs/view/999888")

	b := &Bot{cfg: Config{}, stopCh: make(chan struct{})}
	lazy := &lazyDocGen{
		b: b,
		// Declared intent: correct job 123456789
		job: linkedInJob{ID: "123456789", URL: srv.URL + "/jobs/view/123456789"},
	}

	err := b.easyApply(context.Background(), page, lazy)
	require.Error(t, err, "easyApply must return an error when page is at wrong job URL")
	assert.Contains(t, err.Error(), "job identity", "error must identify this as an identity failure")
}

// ── Identity drift — DB outcome ───────────────────────────────────────────────

// TestSubmitEasyApply_RedirectDriftNoAppliedRecord proves that when the browser
// follows a redirect from the declared job URL to a different job's URL,
// submitEasyApply detects the drift, returns false, and writes no jobs_applied
// record. The identity check fires at entry (before jsPoll) because the page lands
// at the wrong job path immediately after navigation.
func TestSubmitEasyApply_RedirectDriftNoAppliedRecord(t *testing.T) {
	br := launchTestBrowser(t)

	// Serve the declared job URL as a 302 redirect to the wrong job.
	mux := http.NewServeMux()
	mux.HandleFunc("/jobs/view/123456789", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/jobs/view/999888", http.StatusFound)
	})
	mux.HandleFunc("/jobs/view/999888", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, wrongJobPageHTML)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	defer db.Close()

	b := &Bot{
		cfg:    Config{DB: db, UserID: "u1", Platform: domain.PlatformLinkedIn},
		stopCh: make(chan struct{}),
	}
	job := linkedInJob{
		ID:      "123456789",
		URL:     srv.URL + "/jobs/view/123456789",
		Title:   "Test Role",
		Company: "TestCo",
	}
	lazy := &lazyDocGen{b: b, job: job}

	result := b.submitEasyApply(context.Background(), br, job, lazy, 8, "good match", nil, llm.UsageSnapshot{})
	assert.False(t, result, "submitEasyApply must return false when redirect causes drift at entry")

	// No jobs_applied row must exist — redirect to wrong job must not produce an Applied record.
	var appliedCount int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM jobs_applied WHERE link = ? AND user_id = ?`,
		job.URL, "u1",
	).Scan(&appliedCount))
	assert.Equal(t, 0, appliedCount, "jobs_applied must remain empty after redirect drift")

	// apply_attempts must record OutcomeFailed.
	var outcome string
	require.NoError(t, db.QueryRow(
		`SELECT outcome FROM apply_attempts WHERE url = ? AND user_id = ?`,
		job.URL, "u1",
	).Scan(&outcome))
	assert.Equal(t, string(OutcomeBlocked), outcome, "outcome must be failed after redirect drift")
}

// TestSubmitEasyApply_DriftAfterEntry_NoAppliedRecord proves that when the entry
// check passes (page loads at the declared job URL) but the CTA click navigates to
// a different job's modal URL, the post-modal identity check catches the drift,
// submitEasyApply returns false, and no jobs_applied record is written. This covers
// the SDUI preload link scenario where jsPoll follows an onclick that changes the URL.
func TestSubmitEasyApply_DriftAfterEntry_NoAppliedRecord(t *testing.T) {
	br := launchTestBrowser(t)

	// Correct job page: has a data-live-test-easy-apply button whose onclick navigates
	// to the wrong job URL. Entry check passes; drift is detected post-modal.
	const correctJobHTML = `<!DOCTYPE html><html><body>
<main data-job-id="123456789">
  <h1>Test Role at TestCo</h1>
  <button data-live-test-easy-apply
          onclick="window.location.href='/jobs/view/999888'">Easy Apply</button>
</main>
</body></html>`

	// Wrong job modal page: satisfies jsModalPresent via data-live-test-easy-apply-next-button.
	const wrongJobModalHTML = `<!DOCTYPE html><html><body>
<div role="dialog" aria-label="Easy Apply">
  <button data-live-test-easy-apply-next-button>Next</button>
</div>
</body></html>`

	mux := http.NewServeMux()
	mux.HandleFunc("/jobs/view/123456789", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, correctJobHTML)
	})
	mux.HandleFunc("/jobs/view/999888", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, wrongJobModalHTML)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	defer db.Close()

	b := &Bot{
		cfg:    Config{DB: db, UserID: "u1", Platform: domain.PlatformLinkedIn},
		stopCh: make(chan struct{}),
	}
	job := linkedInJob{
		ID:      "123456789",
		URL:     srv.URL + "/jobs/view/123456789",
		Title:   "Test Role",
		Company: "TestCo",
	}
	lazy := &lazyDocGen{b: b, job: job}

	result := b.submitEasyApply(context.Background(), br, job, lazy, 8, "good match", nil, llm.UsageSnapshot{})
	assert.False(t, result, "submitEasyApply must return false when drift occurs after entry")

	var appliedCount int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM jobs_applied WHERE link = ? AND user_id = ?`,
		job.URL, "u1",
	).Scan(&appliedCount))
	assert.Equal(t, 0, appliedCount, "jobs_applied must remain empty after post-entry drift")

	var outcome string
	require.NoError(t, db.QueryRow(
		`SELECT outcome FROM apply_attempts WHERE url = ? AND user_id = ?`,
		job.URL, "u1",
	).Scan(&outcome))
	assert.Equal(t, string(OutcomeBlocked), outcome, "outcome must be failed after post-entry drift")
}

// ── Post-apply drift path ─────────────────────────────────────────────────────

// TestSubmitEasyApply_PostDriftComparedAgainstDeclaredJob verifies that the
// post-apply drift check in submitEasyApply uses the declared job.URL rather
// than the live captured URL. Uses linkedInJobPath directly (same function used
// in submitEasyApply) so the test exercises production code, not a copy.
func TestSubmitEasyApply_PostDriftComparedAgainstDeclaredJob(t *testing.T) {
	cases := []struct {
		name        string
		declaredURL string // corresponds to job.URL (the intent)
		postURL     string // page URL after easyApply succeeds
		wantDrift   bool
	}{
		{
			name:        "same job — no drift",
			declaredURL: "https://www.linkedin.com/jobs/view/123456789",
			postURL:     "https://www.linkedin.com/jobs/view/123456789?openSDUIApplyFlow=true",
			wantDrift:   false,
		},
		{
			name:        "different job — drift detected",
			declaredURL: "https://www.linkedin.com/jobs/view/123456789",
			postURL:     "https://www.linkedin.com/jobs/view/999888",
			wantDrift:   true,
		},
		{
			name:        "empty post URL — drift (fail-closed: unreadable URL is treated as drift)",
			declaredURL: "https://www.linkedin.com/jobs/view/123456789",
			postURL:     "",
			wantDrift:   true,
		},
		{
			name:        "non-LinkedIn URLs — no drift (path extraction yields empty)",
			declaredURL: "https://www.seek.com.au/job/111",
			postURL:     "https://www.seek.com.au/job/222",
			wantDrift:   false,
		},
		{
			name:        "declared has no job path — no drift",
			declaredURL: "https://www.linkedin.com/jobs/collections/recommended/?currentJobId=123",
			postURL:     "https://www.linkedin.com/jobs/view/999888",
			wantDrift:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			intendedPath := linkedInJobPath(tc.declaredURL)
			endPath := linkedInJobPath(tc.postURL)
			drift := intendedPath != "" && (endPath == "" || endPath != intendedPath)
			assert.Equal(t, tc.wantDrift, drift)
		})
	}
}

// TestIdentityDriftError_RoutingProperties verifies that an identity drift
// error from easyApply is routed as a plain apply_error (not mis-classified as
// "not automatable"), and is recognizable in error messages.
func TestIdentityDriftError_RoutingProperties(t *testing.T) {
	driftErr := fmt.Errorf("job identity: page is at %s (expected %s) — aborting before CTA interaction",
		"https://www.linkedin.com/jobs/view/999888",
		"https://www.linkedin.com/jobs/view/123456789")
	reason := "easy apply: " + driftErr.Error()

	// Drift errors must not be silently routed to Top Matches as "not automatable".
	assert.False(t, skipReasonBelongsInTopMatches(reason),
		"identity drift must not be mis-classified as a blocked/external-ATS error")

	// The outcome must be OutcomeFailed (not OutcomeBlocked or OutcomeSubmittedConfirmed).
	assert.Equal(t, OutcomeFailed, classifyApplyOutcome(driftErr))
}
