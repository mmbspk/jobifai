package bot

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/browser"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/jobpage"
)

// Sanitized structure observed in the production worker on 26 November 2026:
// the owned SDUI header uses a paragraph rather than h1, and its description
// is a separate job-ID-tagged section. Opaque generated classes are omitted.
func productionParagraphHeaderFixture(documentTitle string) string {
	return fmt.Sprintf(`<!doctype html><html><head><title>%s</title></head><body>
<main><div data-testid="lazy-column">
<section id="JobDetails_ManageJobBanner_4491867472"></section>
<div><a aria-label="Company, Midnite.">Midnite</a><p>Senior Backend Engineer</p>
<button onclick="localStorage.setItem('unexpected-apply','true')">Apply</button></div>
<section id="JobDetails_AboutTheJob_4491867472"><p>%s</p></section>
<section id="JobDetailsSimilarJobsSlot_4491867472"><h2>More jobs</h2><p>CTO</p></section>
</div></main></body></html>`, html.EscapeString(documentTitle), strings.Repeat("Develop reliable backend services and operate current production systems. ", 8))
}

func TestJobPageURLOnlyParagraphHeaderHasVerifiedTitleAndCapture(t *testing.T) {
	database := pageHookDB(t)
	manager := pageHookManager(t, database)
	br := workerFixtureBrowser(t, func(string) string {
		return productionParagraphHeaderFixture("Senior Backend Engineer | Midnite | LinkedIn")
	})
	job := linkedInJob{URL: "https://www.linkedin.com/jobs/view/4491867472/"}
	page := navigateAndWait(t, br, job.URL)
	bot := New(Config{UserID: pageTestUser, JobPageReader: manager.pageReader, JobPageStore: manager.pageStore})
	result, err := bot.readJobPage(t.Context(), page, domain.PlatformLinkedIn, job)
	require.NoError(t, err)
	require.True(t, result.Identity.Verified)
	require.Equal(t, "Senior Backend Engineer", result.Identity.Title)
	require.Equal(t, "Midnite", result.Identity.Company)
	require.Zero(t, result.Metrics.HTTPAttempts)

	target, _, err := jobpage.NormalizeTarget(jobpage.Target{URL: job.URL})
	require.NoError(t, err)
	reader := jobpage.NewBrowserPage(page, jobpage.Baseline{Verify: func(ctx context.Context, target jobpage.Target) (jobpage.Identity, error) {
		r, err := evalLinkedInJob(page.Context(ctx), target.JobID, false, false, linkedInJob{URL: target.URL})
		return pageIdentity(r, target), err
	}})
	snapshot, err := reader.Capture(t.Context(), target, jobpage.DefaultLimits())
	require.NoError(t, err, "URL-only reads must retain identity proof when native description reading needs recovery")
	require.True(t, snapshot.Identity.Verified)
	require.Equal(t, "Senior Backend Engineer", snapshot.Identity.Title)
	clicked, err := page.Eval("()=>localStorage.getItem('unexpected-apply')==='true'")
	require.NoError(t, err)
	require.False(t, clicked.Value.Bool())
}

func TestJobPageDocumentTitleCannotInventParagraphHeader(t *testing.T) {
	br := workerFixtureBrowser(t, func(string) string { return productionParagraphHeaderFixture("CTO | Midnite | LinkedIn") })
	page := navigateAndWait(t, br, "https://www.linkedin.com/jobs/view/4491867472/")
	for _, title := range []string{"CTO | Midnite | LinkedIn", "Senior Backend Engineer | Another company | LinkedIn"} {
		_, err := page.Eval("(title)=>document.title=title", title)
		require.NoError(t, err)
		r, err := evalLinkedInJob(page, "4491867472", false, false, linkedInJob{})
		require.NoError(t, err)
		require.Empty(t, r.Title, "a metadata hint must also match the visible owned header and company")
	}
}

func TestJobPageTitleHintPreservesDeclaredCallerFallback(t *testing.T) {
	br := workerFixtureBrowser(t, func(string) string {
		return strings.Replace(productionParagraphHeaderFixture("Senior Backend Engineer | Midnite | LinkedIn"), "<p>Senior Backend Engineer</p>", "<a href='/jobs/view/4491867472'><p>Senior Backend Engineer</p></a>", 1)
	})
	page := navigateAndWait(t, br, "https://www.linkedin.com/jobs/view/4491867472/")
	job := linkedInJob{Title: "Senior Backend", Company: "Midnite"}
	r, err := evalLinkedInJob(page, "4491867472", false, false, job)
	require.NoError(t, err)
	require.True(t, r.IdentityVerified)
	require.Empty(t, r.Title, "URL-only metadata recovery must preserve existing declared-title behavior")
	require.Equal(t, job.Title, pageIdentity(r, jobpage.Target{Title: job.Title}).Title)
}

func TestJobPageSEEKCanaryRecoversCompatibilityAcrossCanonicalAlias(t *testing.T) {
	database := pageHookDB(t)
	manager := pageHookManager(t, database)
	br := workerFixtureBrowser(t, func(raw string) string {
		u, err := url.Parse(raw)
		require.NoError(t, err)
		if u.Path == "/browser-upgrade" {
			return seekCompatibilityFixture
		}
		if strings.HasPrefix(u.Path, "/job/") {
			if u.Hostname() == "www.seek.com.au" {
				return fmt.Sprintf("<script>location.replace(%q)</script>", "https://au.seek.com"+u.Path)
			}
			return "<script>if(!document.cookie.includes('UpdateBrowserMessage=1'))location.replace('/browser-upgrade?continue='+encodeURIComponent(location.href));</script>" +
				strings.ReplaceAll(pageHookHTML("seek", seekJobIDFromURL(raw), strings.Repeat("Current listing "+seekJobIDFromURL(raw)+" responsibilities and requirements. ", 8)), "sessionStorage.setItem('read-only-apply-click','true')", "localStorage.setItem('unexpected-apply','true')")
		}
		return "<main>SEEK home</main>"
	})
	manager.seekBrowsers[pageTestUser] = br
	for _, id := range []string{"22345678", "22345679"} {
		result, err := manager.ReadJobPage(t.Context(), pageTestUser, "https://www.seek.com.au/job/"+id)
		require.NoError(t, err)
		require.Equal(t, id, result.Identity.JobID)
		require.Contains(t, result.Description, "Current listing "+id)
		require.Equal(t, jobpage.SourceDeterministic, result.Source)
		require.Zero(t, result.Metrics.HTTPAttempts)
	}
	page := navigateAndWait(t, br, "https://au.seek.com/job/22345679")
	state, err := page.Eval("()=>[localStorage.getItem('ackTrusted'),localStorage.getItem('unexpected-apply')]")
	require.NoError(t, err)
	require.Equal(t, "true", state.Value.Get("0").String())
	require.NotEqual(t, "true", state.Value.Get("1").String())
	var applied int
	require.NoError(t, database.QueryRow("SELECT COUNT(*) FROM jobs_applied").Scan(&applied))
	require.Zero(t, applied)
}

func TestSeekCompatibilityRequiresSameIssuerAndIntendedListing(t *testing.T) {
	br := workerFixtureBrowser(t, func(string) string { return seekCompatibilityFixture })
	for _, next := range []string{"https://nz.seek.com/job/123", "https://au.seek.com/job/456", "https://au.seek.com:9443/job/123"} {
		t.Run(next, func(t *testing.T) {
			page := navigateAndWait(t, br, "https://au.seek.com/browser-upgrade?continue="+url.QueryEscape(next))
			err := browser.RecoverSeekBrowserUpgrade(page, "https://www.seek.com.au/job/123")
			require.ErrorIs(t, err, browser.ErrSeekBrowserUnsupported)
			value, err := page.Eval("()=>document.cookie")
			require.NoError(t, err)
			require.NotContains(t, value.Value.String(), "UpdateBrowserMessage")
		})
	}
}

func TestJobPageSEEKClientRenderedSubjectReachesURLAndPreparationReaders(t *testing.T) {
	database := pageHookDB(t)
	manager := pageHookManager(t, database)
	jobURL := "https://au.seek.com/job/95987525"
	description := strings.Repeat("Verified employer requirements for the current Backend Developer vacancy. ", 8)
	br := workerFixtureBrowser(t, func(string) string {
		return "<main><h1 data-automation='job-detail-title'>Backend Developer</h1><span data-automation='advertiser-name'>m-View Live Video Pty Ltd</span>" +
			"<a data-automation='job-detail-apply' href='https://au.seek.com/job/95987525/apply' onclick=\"localStorage.setItem('unexpected-apply','true');return false\">Quick apply</a>" +
			"<div data-automation='jobAdDetails'><h2>Backend Developer</h2><p>" + description + "</p></div></main>"
	})
	manager.seekBrowsers[pageTestUser] = br
	result, err := manager.ReadJobPage(t.Context(), pageTestUser, jobURL)
	require.NoError(t, err)
	require.Equal(t, "95987525", result.Identity.JobID)
	require.Equal(t, "Backend Developer", result.Identity.Title)
	require.Equal(t, jobpage.SourceDeterministic, result.Source)
	require.Zero(t, result.Metrics.HTTPAttempts)
	page := navigateAndWait(t, br, jobURL)
	bot := New(Config{UserID: pageTestUser, JobPageReader: manager.pageReader, JobPageStore: manager.pageStore})
	lazy, err := managerLazyWithReadError(bot, t.Context(), SubmitRequest{JobID: "cannot-apply-business-record", Platform: "seek", Link: jobURL, Role: "Backend Developer", Company: "m-View Live Video Pty Ltd"}, page)
	require.NoError(t, err)
	require.Contains(t, lazy.jobDesc, "Verified employer requirements")
	clicked, err := page.Eval("()=>localStorage.getItem('unexpected-apply')==='true'")
	require.NoError(t, err)
	require.False(t, clicked.Value.Bool())
}
