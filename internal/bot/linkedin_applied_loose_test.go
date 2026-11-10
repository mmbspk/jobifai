package bot

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLinkedInAppliedLoose_RequiresVerifiedJobIdentity(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	// Collection URL with no path job id; a visible Applied control for job 999 must not match job 111.
	mux.HandleFunc("/jobs/collections/recommended/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html><html><body>
<main data-job-id="999">
  <button aria-label="Applied">Applied</button>
  <p>You applied to this role 2 days ago</p>
</main>
</body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	page := navigateAndWait(t, launchTestBrowser(t), srv.URL+"/jobs/collections/recommended/?currentJobId=111")
	ok, err := page.Eval(jsLinkedInAppliedLoose, "111")
	require.NoError(t, err)
	assert.False(t, ok.Value.Bool(), "must not treat unscoped Applied evidence as intended job 111")
}

func TestLinkedInSubmitReconciled_AcceptsVerifiedAlreadyApplied(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/jobs/view/111", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html><html><body>
<main data-job-id="111">
  <h1><a href="/jobs/view/111">Analyst</a></h1>
  <span aria-label="Company, Example Co">Example Co</span>
  <button aria-label="Applied" data-test-job-apply-button-applied>Applied</button>
</main>
</body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	jobURL := srv.URL + "/jobs/view/111"
	page := navigateAndWait(t, launchTestBrowser(t), jobURL)
	require.True(t, linkedInSubmitReconciled(page, jobURL, "Example Co"))
}

func TestLinkedInAppliedLoose_MatchesVerifiedAlreadyApplied(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/jobs/view/111", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html><html><body>
<main data-job-id="111">
  <h1><a href="/jobs/view/111">Analyst</a></h1>
  <span aria-label="Company, Example Co">Example Co</span>
  <button aria-label="Applied" data-test-job-apply-button-applied>Applied</button>
</main>
</body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	page := navigateAndWait(t, launchTestBrowser(t), srv.URL+"/jobs/view/111")
	ok, err := page.Eval(jsLinkedInAppliedLoose, "111")
	require.NoError(t, err)
	assert.True(t, ok.Value.Bool())
}
