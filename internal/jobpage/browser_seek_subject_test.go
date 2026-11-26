package jobpage

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Production SEEK renders this primary tuple without JobPosting JSON-LD or
// data-job-id. The job-specific apply reference is observed but never clicked.
func seekClientRenderedFixture(reference string) string {
	return fmt.Sprintf(`<main><header>
<h1 data-automation="job-detail-title">Backend Developer</h1>
<span data-automation="advertiser-name">m-View Live Video Pty Ltd</span>
<a data-automation="job-detail-apply" href="%s" onclick="window.applicationClicked=true;return false">Quick apply</a>
</header><div data-automation="jobAdDetails"><h2>Backend Developer</h2><p>%s</p></div>
<section data-testid="competitive-jobs"><h3>Featured jobs</h3>
<span data-automation="advertiser-name">Foreign employer</span>
<a data-automation="job-detail-apply" href="/job/95987525/apply">Quick apply</a>
<div data-automation="jobAdDetails">FOREIGN RECOMMENDATION TEXT</div></section></main>`,
		reference, strings.Repeat("Develop backend services and operate the current employer's production systems. ", 8))
}

func TestBrowserSEEKClientRenderedSubjectWithoutJSONLD(t *testing.T) {
	browser := readerFixtureBrowser(t)
	target, _, err := NormalizeTarget(Target{URL: "https://au.seek.com/job/95987525", Title: "Backend Developer", Company: "m-View Live Video Pty Ltd"})
	require.NoError(t, err)
	reader, page := readerFixture(t, browser, target, seekClientRenderedFixture("https://au.seek.com/job/95987525/apply?tracking_token=PRIVATE_REFERENCE_MARKER"))
	result, err := reader.Known(t.Context(), target, DefaultLimits())
	require.NoError(t, err)
	require.True(t, result.Identity.Verified)
	require.Equal(t, "95987525", result.Identity.JobID)
	require.Equal(t, target.Title, result.Identity.Title)
	require.Equal(t, target.Company, result.Identity.Company)
	require.Contains(t, result.Description, "Backend Developer")
	require.Contains(t, result.Description, "current employer's production systems")
	require.NotContains(t, result.Description, "FOREIGN")
	require.Contains(t, result.Identity.Evidence, "job_detail_apply_reference")
	snapshot, err := reader.Capture(t.Context(), target, DefaultLimits())
	require.NoError(t, err)
	raw, err := json.Marshal(snapshot)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "PRIVATE_REFERENCE_MARKER")
	clicked, err := page.Eval("()=>window.applicationClicked===true")
	require.NoError(t, err)
	require.False(t, clicked.Value.Bool())
}

func TestBrowserSEEKClientRenderedSubjectRejectsUncorroboratedReferences(t *testing.T) {
	browser := readerFixtureBrowser(t)
	target := Target{URL: "https://au.seek.com/job/95987525"}
	for _, test := range []struct{ name, reference string }{
		{"another job", "/job/456/apply"},
		{"another issuer", "https://nz.seek.com/job/95987525/apply"},
		{"another port", "https://au.seek.com:9443/job/95987525/apply"},
		{"insecure reference", "http://au.seek.com/job/95987525/apply"},
		{"credential-bearing reference", "https://user:private@au.seek.com/job/95987525/apply"},
		{"missing reference", ""},
		{"query-only reference", "?job_id=95987525"},
		{"fragment-only reference", "#apply"},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader, page := readerFixture(t, browser, target, seekClientRenderedFixture(test.reference))
			_, err := reader.Capture(t.Context(), target, DefaultLimits())
			require.Equal(t, FailureIdentity, ErrorCode(err))
			clicked, err := page.Eval("()=>window.applicationClicked===true")
			require.NoError(t, err)
			require.False(t, clicked.Value.Bool())
		})
	}
	t.Run("recommendation cannot lend its reference", func(t *testing.T) {
		html := strings.Replace(seekClientRenderedFixture("/job/95987525/apply"), "data-automation=\"job-detail-apply\"", "data-automation=\"unrelated-link\"", 1)
		reader, _ := readerFixture(t, browser, target, html)
		_, err := reader.Capture(t.Context(), target, DefaultLimits())
		require.Equal(t, FailureIdentity, ErrorCode(err))
	})
	t.Run("description cannot lend its title", func(t *testing.T) {
		html := strings.Replace(seekClientRenderedFixture("/job/95987525/apply"), "<h1 data-automation=\"job-detail-title\">Backend Developer</h1>", "", 1)
		html = strings.Replace(html, "<h2>Backend Developer</h2>", "<h2 data-automation=\"job-detail-title\">Backend Developer</h2>", 1)
		reader, _ := readerFixture(t, browser, target, html)
		_, err := reader.Capture(t.Context(), target, DefaultLimits())
		require.Equal(t, FailureIdentity, ErrorCode(err))
	})
}

func TestSEEKApplyReferenceEvidenceCannotAuthorizeAnotherPlatform(t *testing.T) {
	snapshot := fixtureSnapshot()
	snapshot.Identity.Evidence = []string{"job_detail_apply_reference", "primary_header"}
	require.Equal(t, FailureIdentity, ErrorCode(ValidateSnapshot(snapshot, DefaultLimits())))
}
