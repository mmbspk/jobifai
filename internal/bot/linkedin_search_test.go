package bot

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLinkedInSearchJobsJSON_ClassicAndSDUI(t *testing.T) {
	t.Parallel()
	raw := `[{"id":"111","title":"Engineer","company":"Acme","location":"Adelaide","url":"https://www.linkedin.com/jobs/view/111/","easyApply":true,"alreadyApplied":false},
{"id":"222","title":"Lead","company":"Beta","location":"","url":"","easyApply":false,"alreadyApplied":true}]`
	jobs := parseLinkedInSearchJobsJSON(raw, "Remote")
	require.Len(t, jobs, 2)
	assert.Equal(t, "111", jobs[0].ID)
	assert.True(t, jobs[0].EasyApply)
	assert.Equal(t, "222", jobs[1].ID)
	assert.Equal(t, "Remote", jobs[1].Location)
	assert.True(t, jobs[1].AlreadyApplied)
}

func TestLinkedInSearchJobs_SDUIFixture(t *testing.T) {
	t.Parallel()
	br := launchTestBrowser(t)
	html := `<main><div class="job-card-container" data-job-id="999888777">
   <a class="job-card-container__link" href="/jobs/view/999888777/"><span class="job-card-list__title">Platform Engineer</span></a>
   <span class="job-card-container__company-name">Widgets Co</span>
  </div></main>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, html)
	}))
	t.Cleanup(srv.Close)
	page := navigateAndWait(t, br, srv.URL)
	val, err := page.Eval(jsLinkedInSearchJobs)
	require.NoError(t, err)
	jobs := parseLinkedInSearchJobsJSON(val.Value.String(), "AU")
	require.Len(t, jobs, 1)
	assert.Equal(t, "999888777", jobs[0].ID)
	assert.Equal(t, "Platform Engineer", jobs[0].Title)
	assert.Equal(t, "Widgets Co", jobs[0].Company)
}

func TestLinkedInSearchJobs_AISearchFixture(t *testing.T) {
	t.Parallel()
	br := launchTestBrowser(t)
	html := `<main>
  <div componentkey="job-card-component-ref-4479755890" role="button">
    <span>Selected, Senior Software Engineer (Verified job)</span>
    <span>Senior Software Engineer</span>
    <span>Babcock Australia &amp; New Zealand</span>
    <span>Black Forest, SA</span>
  </div>
  <div><button aria-label="Dismiss Senior Software Engineer job"></button></div>
  <div componentkey="job-card-component-ref-4484173125" role="button">
    <span>Senior Software Engineer (Verified job)</span>
    <span>Senior Software Engineer</span>
    <span>Topcon Positioning Systems</span>
    <span>Adelaide, SA</span>
    <span>Easy Apply</span>
  </div>
</main>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, html)
	}))
	t.Cleanup(srv.Close)
	page := navigateAndWait(t, br, srv.URL)
	val, err := page.Eval(jsLinkedInSearchJobs)
	require.NoError(t, err)
	jobs := parseLinkedInSearchJobsJSON(val.Value.String(), "AU")
	require.Len(t, jobs, 2)
	assert.Equal(t, "4479755890", jobs[0].ID)
	assert.Equal(t, "Babcock Australia & New Zealand", jobs[0].Company)
	assert.Equal(t, "4484173125", jobs[1].ID)
	assert.True(t, jobs[1].EasyApply)
}
