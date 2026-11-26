package jobpage

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBrowserRejectsForeignJobHeadingWithoutCardAttributes(t *testing.T) {
	browser := readerFixtureBrowser(t)
	for _, platform := range []string{"linkedin", "seek"} {
		t.Run(platform, func(t *testing.T) {
			url, foreign := "https://www.linkedin.com/jobs/view/123/", "/jobs/view/456/"
			if platform == "seek" {
				url, foreign = "https://www.seek.com.au/job/123", "/job/456"
			}
			target, _, err := NormalizeTarget(Target{URL: url, Title: "Engineer", Company: "Acme"})
			require.NoError(t, err)
			html := fixtureHTML("<section data-testid='job-description'>" + fixtureText() + "</section>" + fmt.Sprintf("<section><div><h3><a href='%s'>Engineer</a></h3></div><p>%s</p></section>", foreign, strings.Repeat("FOREIGN JOB RESPONSIBILITIES ", 10)))
			reader, _ := readerFixture(t, browser, target, html)
			snapshot, err := reader.Capture(t.Context(), target, DefaultLimits())
			require.NoError(t, err)
			for _, node := range snapshot.Nodes {
				require.NotContains(t, node.Text, "FOREIGN JOB RESPONSIBILITIES")
			}
			result, err := reader.Known(t.Context(), target, DefaultLimits())
			require.NoError(t, err)
			require.NotContains(t, result.Description, "FOREIGN JOB RESPONSIBILITIES")
		})
	}
}

func TestBrowserForeignIssuerCannotBorrowSameNumericJobID(t *testing.T) {
	browser := readerFixtureBrowser(t)
	target, _, err := NormalizeTarget(Target{URL: "https://www.seek.com.au/job/123", Title: "Engineer", Company: "Acme"})
	require.NoError(t, err)
	html := fixtureHTML("<section data-testid='job-description'>" + fixtureText() + "</section><section><h3><a href='https://www.seek.co.nz/job/123'>Engineer</a></h3><p>" + strings.Repeat("FOREIGN ISSUER RESPONSIBILITIES ", 10) + "</p></section>")
	page, _ := readerFixture(t, browser, target, html)
	snapshot, err := page.Capture(t.Context(), target, DefaultLimits())
	require.NoError(t, err)
	for _, node := range snapshot.Nodes {
		require.NotContains(t, node.Text, "FOREIGN ISSUER RESPONSIBILITIES")
	}
}

func TestBrowserForeignIssuerCannotBorrowMatchingNestedJobAttribute(t *testing.T) {
	browser := readerFixtureBrowser(t)
	target, _, err := NormalizeTarget(Target{URL: "https://www.seek.com.au/job/123", Title: "Engineer", Company: "Acme"})
	require.NoError(t, err)
	html := fixtureHTML("<section data-testid='job-description'>" + fixtureText() + "<section data-job-id='123'><h3><a href='https://www.seek.co.nz/job/123'>Engineer</a></h3><p>" + strings.Repeat("FOREIGN ISSUER RESPONSIBILITIES ", 10) + "</p></section></section>")
	page, _ := readerFixture(t, browser, target, html)
	snapshot, err := page.Capture(t.Context(), target, DefaultLimits())
	require.NoError(t, err)
	for _, node := range snapshot.Nodes {
		require.NotContains(t, node.Text, "FOREIGN ISSUER RESPONSIBILITIES")
	}
	result, err := page.Known(t.Context(), target, DefaultLimits())
	require.NoError(t, err)
	require.Contains(t, result.Description, "Build secure services")
	require.NotContains(t, result.Description, "FOREIGN ISSUER RESPONSIBILITIES")
}
