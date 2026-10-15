package bot

// Browser fixture tests for processJob's listing-unavailable guard.
//
// When LinkedIn redirects an expired job URL to a similar-jobs collections page,
// that page shows a different job's content (description, Easy Apply CTA). These
// tests prove that processJob detects the redirect via verifyJobIdentity and:
//
//   - records the job as skipped with the identity error preserved
//   - never routes the job to Top Matches (jobs_pending_review)
//   - never uses the collections page description for scoring
//   - never attempts an Easy Apply against the wrong listing
//
// A regression guard confirms the happy path is unaffected: a valid non-Easy-Apply
// job page still routes to Top Matches as expected.
//
// Tests are skipped automatically when no Chromium binary is available.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
)

// collectionsPageHTML simulates a LinkedIn similar-jobs collections page.
// It carries a different job's Easy Apply CTA and a description longer than 100
// characters — both must not be attributed to the original (expired) listing.
const collectionsPageHTML = `<!DOCTYPE html><html><body>
<main>
  <h1>Wrong Highlighted Job at WrongCo</h1>
  <button aria-label="Easy Apply" style="display:block; width:120px; height:40px">Easy Apply</button>
  <div>This is the description of the WRONG JOB shown on the collections page.
  It is intentionally longer than one hundred characters so it would corrupt
  the score if identity gating were absent.</div>
</main>
</body></html>`

// expiredJobPageHTML simulates a generic error/unavailable page with no job path.
const expiredJobPageHTML = `<!DOCTYPE html><html><body>
<main><h1>Job no longer available</h1><p>This posting has been removed.</p></main>
</body></html>`

// validExternalApplyHTML: a correct job page at the declared URL, external Apply only.
// Used to prove the happy path — verified listing with no Easy Apply — routes to Top Matches.
const validExternalApplyHTML = `<!DOCTYPE html><html><body>
<main>
  <h1>Test Role at OriginalCo</h1>
  <a href="https://external-ats.example.com/apply">Apply on company website</a>
  <div>This is a genuine job description for the correct listing.
  It is intentionally longer than one hundred characters to ensure description
  extraction works normally for the regression guard test case.</div>
</main>
</body></html>`

// TestProcessJob_CollectionsPageRedirect_IsSkippedNotTopMatches verifies the main
// expired-listing scenario: the original job URL redirects to a similar-jobs
// collections page that highlights a different job's Easy Apply CTA and description.
// The bot must record the original job as skipped (with the identity error preserved)
// and must not place it in Top Matches or attempt an Easy Apply against the wrong job.
func TestProcessJob_CollectionsPageRedirect_IsSkippedNotTopMatches(t *testing.T) {
	br := launchTestBrowser(t)

	mux := http.NewServeMux()
	// Original job URL redirects to a similar-jobs collections page — the collections
	// page highlights a different job (OTHER456) which has an Easy Apply CTA.
	mux.HandleFunc("/jobs/view/ORIG123", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/jobs/collections/similar-jobs/?currentJobId=OTHER456", http.StatusFound)
	})
	mux.HandleFunc("/jobs/collections/similar-jobs/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, collectionsPageHTML)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	b := &Bot{
		cfg:       Config{DB: db, UserID: "u1", Platform: domain.PlatformLinkedIn},
		stopCh:    make(chan struct{}),
		seenCache: newJobSeenCache(),
	}
	job := linkedInJob{
		ID:      "ORIG123",
		URL:     srv.URL + "/jobs/view/ORIG123",
		Title:   "Test Role",
		Company: "OriginalCo",
	}

	result := b.processJob(context.Background(), br, job)
	assert.False(t, result, "processJob must return false for an unavailable listing")

	// The original job must be recorded in jobs_skipped with the identity error preserved.
	var skipReason string
	require.NoError(t, db.QueryRow(
		`SELECT skip_reason FROM jobs_skipped WHERE id = ? AND user_id = ?`,
		job.ID, "u1",
	).Scan(&skipReason), "job must be recorded in jobs_skipped")
	assert.Contains(t, skipReason, "listing unavailable", "skip reason must identify the listing as unavailable")
	assert.Contains(t, skipReason, "no LinkedIn job path", "skip reason must preserve the identity error")

	// Must NOT appear in Top Matches — the job is gone, not a manual-apply opportunity.
	var pendingCount int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM jobs_pending_review WHERE job_id = ? AND user_id = ?`,
		job.ID, "u1",
	).Scan(&pendingCount))
	assert.Equal(t, 0, pendingCount, "unavailable listing must not appear in Top Matches")
}

// TestProcessJob_NonJobPageRedirect_IsSkipped verifies the unreadable-page variant:
// the original job URL redirects to a plain error page (no /jobs/view/ path in the URL).
// The identity check catches the missing job path and records the job as skipped.
func TestProcessJob_NonJobPageRedirect_IsSkipped(t *testing.T) {
	br := launchTestBrowser(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/jobs/view/ORIG123", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/jobs/not-found", http.StatusFound)
	})
	mux.HandleFunc("/jobs/not-found", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, expiredJobPageHTML)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	b := &Bot{
		cfg:       Config{DB: db, UserID: "u1", Platform: domain.PlatformLinkedIn},
		stopCh:    make(chan struct{}),
		seenCache: newJobSeenCache(),
	}
	job := linkedInJob{
		ID:      "ORIG123",
		URL:     srv.URL + "/jobs/view/ORIG123",
		Title:   "Test Role",
		Company: "OriginalCo",
	}

	result := b.processJob(context.Background(), br, job)
	assert.False(t, result)

	var skipReason string
	require.NoError(t, db.QueryRow(
		`SELECT skip_reason FROM jobs_skipped WHERE id = ? AND user_id = ?`,
		job.ID, "u1",
	).Scan(&skipReason), "job must be recorded in jobs_skipped")
	assert.Contains(t, skipReason, "listing unavailable")

	var pendingCount int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM jobs_pending_review WHERE job_id = ? AND user_id = ?`,
		job.ID, "u1",
	).Scan(&pendingCount))
	assert.Equal(t, 0, pendingCount, "unavailable listing must not appear in Top Matches")
}

// capturingScorer is a JobScorer that records the job description it receives.
// Used to assert that the description reaching the scorer is the browser-derived
// or title+company fallback, never HTTP content fetched without identity checks.
type capturingScorer struct {
	gotDesc string
}

func (s *capturingScorer) EvaluateJob(_ context.Context, _ *domain.ResumeProfile, jobDesc string) (domain.JobScore, error) {
	s.gotDesc = jobDesc
	return domain.JobScore{Score: 8}, nil
}

// TestProcessJob_BrowserOpenFails_IsSkipped verifies that when the browser cannot open
// a page at all (connection refused / browser closed), processJob records the job as
// skipped rather than falling through to the removed HTTP fallback or returning true.
func TestProcessJob_BrowserOpenFails_IsSkipped(t *testing.T) {
	br := launchTestBrowser(t)
	// Close the browser so that br.Page() returns a connection error.
	require.NoError(t, br.Close())

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	b := &Bot{
		cfg:       Config{DB: db, UserID: "u1", Platform: domain.PlatformLinkedIn},
		stopCh:    make(chan struct{}),
		seenCache: newJobSeenCache(),
	}
	job := linkedInJob{
		ID:      "ORIG123",
		URL:     "https://www.linkedin.com/jobs/view/ORIG123",
		Title:   "Test Role",
		Company: "OriginalCo",
	}

	result := b.processJob(context.Background(), br, job)
	assert.False(t, result, "processJob must return false when browser page open fails")

	var skipReason string
	require.NoError(t, db.QueryRow(
		`SELECT skip_reason FROM jobs_skipped WHERE id = ? AND user_id = ?`,
		job.ID, "u1",
	).Scan(&skipReason), "job must be recorded in jobs_skipped")
	assert.Contains(t, skipReason, "browser page open failed", "skip reason must identify the browser failure")

	var pendingCount int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM jobs_pending_review WHERE job_id = ? AND user_id = ?`,
		job.ID, "u1",
	).Scan(&pendingCount))
	assert.Equal(t, 0, pendingCount, "job with browser failure must not appear in Top Matches")
}

// TestProcessJob_ThinDescriptionNoHTTPFallback verifies that when the browser page loads
// successfully but yields a description shorter than 100 characters, processJob falls back
// to "title at company" rather than making an HTTP fetch. An HTTP fetch would follow
// redirects without identity checks and could return collections page content.
func TestProcessJob_ThinDescriptionNoHTTPFallback(t *testing.T) {
	br := launchTestBrowser(t)

	// Serve a valid job page with a description too short to pass the 100-char threshold.
	const thinDescHTML = `<!DOCTYPE html><html><body>
<main>
  <h1>Test Role at OriginalCo</h1>
  <div>Short description.</div>
</main>
</body></html>`

	mux := http.NewServeMux()
	mux.HandleFunc("/jobs/view/ORIG123", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, thinDescHTML)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	scorer := &capturingScorer{}
	b := &Bot{
		cfg:       Config{DB: db, UserID: "u1", Platform: domain.PlatformLinkedIn, Scorer: scorer},
		stopCh:    make(chan struct{}),
		seenCache: newJobSeenCache(),
	}
	job := linkedInJob{
		ID:      "ORIG123",
		URL:     srv.URL + "/jobs/view/ORIG123",
		Title:   "Test Role",
		Company: "OriginalCo",
	}

	b.processJob(context.Background(), br, job)

	assert.Equal(t, "Test Role at OriginalCo", scorer.gotDesc,
		"thin browser description must fall back to title+company, not HTTP-fetched content")
}

// TestProcessJob_ValidNonEasyApplyJob_GoesToTopMatches is the regression guard:
// a valid listing that loads at the declared URL with only an external Apply button
// must still route to Top Matches (jobs_pending_review, easy_apply=0) as before.
func TestProcessJob_ValidNonEasyApplyJob_GoesToTopMatches(t *testing.T) {
	br := launchTestBrowser(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/jobs/view/ORIG123", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, validExternalApplyHTML)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	b := &Bot{
		cfg:       Config{DB: db, UserID: "u1", Platform: domain.PlatformLinkedIn},
		stopCh:    make(chan struct{}),
		seenCache: newJobSeenCache(),
	}
	job := linkedInJob{
		ID:      "ORIG123",
		URL:     srv.URL + "/jobs/view/ORIG123",
		Title:   "Test Role",
		Company: "OriginalCo",
	}

	result := b.processJob(context.Background(), br, job)
	assert.False(t, result, "non-Easy Apply returns false (queued to Top Matches, not applied)")

	// Valid listing must be in Top Matches with easy_apply=0.
	var easyApply int
	require.NoError(t, db.QueryRow(
		`SELECT easy_apply FROM jobs_pending_review WHERE job_id = ? AND user_id = ?`,
		job.ID, "u1",
	).Scan(&easyApply), "valid non-Easy Apply listing must be in jobs_pending_review")
	assert.Equal(t, 0, easyApply, "easy_apply must be 0 for a non-Easy Apply listing in Top Matches")

	// Must not be skipped.
	var skipCount int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM jobs_skipped WHERE id = ? AND user_id = ?`,
		job.ID, "u1",
	).Scan(&skipCount))
	assert.Equal(t, 0, skipCount, "valid listing must not be in jobs_skipped")
}
