package jobpage

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/stretchr/testify/require"
)

func readerFixtureBrowser(t *testing.T) *rod.Browser {
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
		t.Skipf("Chromium unavailable: %v", err)
	}
	b := rod.New().ControlURL(u)
	require.NoError(t, b.Connect())
	t.Cleanup(func() { _ = b.Close(); l.Cleanup() })
	return b
}

func readerFixture(t *testing.T, b *rod.Browser, target Target, html string) (*BrowserPage, *rod.Page) {
	t.Helper()
	page, err := b.Page(proto.TargetCreateTarget{URL: "about:blank"})
	require.NoError(t, err)
	router := page.HijackRequests()
	require.NoError(t, router.Add("*", "", func(h *rod.Hijack) {
		h.Response.SetHeader("Content-Type", "text/html; charset=utf-8").SetBody("<!doctype html><html><head></head><body>" + html + "</body></html>")
	}))
	go router.Run()
	t.Cleanup(func() { _ = router.Stop(); _ = page.Close() })
	require.NoError(t, page.Navigate(target.URL))
	require.NoError(t, page.Timeout(5*time.Second).WaitLoad())
	return NewBrowserPage(page, Baseline{}), page
}

func fixtureTarget() Target {
	return Target{Platform: "linkedin", URL: "https://www.linkedin.com/jobs/view/123/", JobID: "123", Title: "Engineer", Company: "Acme"}
}
func fixtureText() string {
	return strings.Repeat("Build secure services, own reliable releases, and collaborate with the engineering team. ", 5)
}
func fixtureHTML(description string) string {
	return `<main data-job-id="123"><h1>Engineer</h1><a aria-label="Company, Acme.">Acme</a><button onclick="window.applied=true">Easy Apply</button>` + description + `</main>`
}
func fixturePlan(s Snapshot) Plan {
	return Plan{Version: SnapshotVersion, SnapshotID: s.ID, DescriptionRefs: s.Candidates}
}

func TestBrowserPageOwnedFixtures(t *testing.T) {
	b := readerFixtureBrowser(t)
	ctx := context.Background()
	limits := DefaultLimits()
	t.Run("linkedin known description excludes unrelated and hidden sections", func(t *testing.T) {
		html := fixtureHTML(`<section data-testid="job-description"><p>` + fixtureText() + `</p><p hidden>PRIVATE HIDDEN TEXT</p><section><h2>About the company</h2><p>UNRELATED COMPANY</p></section><aside>WRONG RECOMMENDATION</aside><section data-job-id="999">FOREIGN LISTING</section></section><form><input value="PRIVATE PROFILE"></form>`)
		p, page := readerFixture(t, b, fixtureTarget(), html)
		r, err := p.Known(ctx, fixtureTarget(), limits)
		require.NoError(t, err)
		require.Equal(t, fixtureText(), r.Description+" ")
		require.Equal(t, SourceDeterministic, r.Source)
		value, err := page.Eval(`()=>window.applied===true`)
		require.NoError(t, err)
		require.False(t, value.Value.Bool())
	})
	t.Run("seek structured subject and visible header", func(t *testing.T) {
		target := Target{Platform: "seek", URL: "https://www.seek.com.au/job/123", JobID: "123", Title: "Engineer", Company: "Acme"}
		html := `<script type="application/ld+json">{"@type":"JobPosting","identifier":{"value":"123"},"title":"Engineer","hiringOrganization":{"name":"Acme"},"datePosted":"2026-11-01"}</script><main><h1 data-automation="job-detail-title">Engineer</h1><a data-automation="advertiser-name">Acme</a><section data-automation="jobAdDetails">` + fixtureText() + `</section></main>`
		p, _ := readerFixture(t, b, target, html)
		r, err := p.Known(ctx, target, limits)
		require.NoError(t, err)
		require.Equal(t, "2026-11-01", r.PostedDate)
	})
	t.Run("sibling SDUI description and display contents", func(t *testing.T) {
		html := `<main><div data-testid="lazy-column"><section id="JobDetails_ManageJobBanner_123"><p>Engineer</p><a aria-label="Company, Acme.">Acme</a></section><section id="JobDetails_AboutTheJob_123" style="display:contents"><p>` + fixtureText() + `</p></section><section id="JobDetailsSimilarJobsSlot_123">WRONG JOB</section></div></main>`
		p, _ := readerFixture(t, b, fixtureTarget(), html)
		r, err := p.Known(ctx, fixtureTarget(), limits)
		require.NoError(t, err)
		require.NotContains(t, r.Description, "WRONG")
	})
	t.Run("captured production SDUI structure", func(t *testing.T) {
		html, err := os.ReadFile("../bot/testdata/linkedin/sdui-native.html")
		require.NoError(t, err)
		target := Target{URL: "https://www.linkedin.com/jobs/view/4477930880/", Title: "Principal Software Engineer (EL2) | Federal Government | Contract", Company: "Exceed Global"}
		p, _ := readerFixture(t, b, target, string(html))
		r, err := p.Known(ctx, target, limits)
		require.NoError(t, err)
		require.Contains(t, r.Description, "Set standards for code quality")
		require.NotContains(t, r.Description, "Tanda")
	})
	t.Run("unfamiliar owned candidate needs interpretation", func(t *testing.T) {
		p, _ := readerFixture(t, b, fixtureTarget(), fixtureHTML(`<article data-test="new-job-copy"><p>`+fixtureText()+`</p></article>`))
		_, err := p.Known(ctx, fixtureTarget(), limits)
		require.Equal(t, FailureMissing, ErrorCode(err))
		s, err := p.Capture(ctx, fixtureTarget(), limits)
		require.NoError(t, err)
		require.Len(t, s.Candidates, 1)
		n, _ := s.Node(s.Candidates[0])
		require.Equal(t, ContextCandidate, n.Context)
		r, err := p.Read(ctx, fixtureTarget(), s, fixturePlan(s), limits)
		require.NoError(t, err)
		require.Contains(t, r.Description, "Build secure services")
	})
}

func TestBrowserPageRejectsUnsafeReads(t *testing.T) {
	b := readerFixtureBrowser(t)
	ctx := context.Background()
	limits := DefaultLimits()
	t.Run("wrong subject despite matching URL and title", func(t *testing.T) {
		html := strings.ReplaceAll(fixtureHTML(`<section data-testid="job-description">`+fixtureText()+`</section>`), `data-job-id="123"`, `data-job-id="999"`)
		p, _ := readerFixture(t, b, fixtureTarget(), html)
		_, err := p.Capture(ctx, fixtureTarget(), limits)
		require.Equal(t, FailureIdentity, ErrorCode(err))
	})
	t.Run("duplicate primary headers conflict", func(t *testing.T) {
		html := fixtureHTML(`<section data-testid="job-description">`+fixtureText()+`</section>`) + fixtureHTML(`<section data-testid="job-description">`+fixtureText()+`</section>`)
		p, _ := readerFixture(t, b, fixtureTarget(), html)
		_, err := p.Capture(ctx, fixtureTarget(), limits)
		require.Equal(t, FailureAmbiguous, ErrorCode(err))
	})
	t.Run("sticky duplicate title accepted but conflicting header rejected", func(t *testing.T) {
		for _, title := range []string{"Engineer", "Different role"} {
			p, _ := readerFixture(t, b, fixtureTarget(), fixtureHTML(`<h1>`+title+`</h1><section data-testid="job-description">`+fixtureText()+`</section>`))
			_, err := p.Known(ctx, fixtureTarget(), limits)
			if title == "Engineer" {
				require.NoError(t, err)
			} else {
				require.Equal(t, FailureIdentity, ErrorCode(err))
			}
		}
	})
	t.Run("conflicting company rejected", func(t *testing.T) {
		p, _ := readerFixture(t, b, fixtureTarget(), fixtureHTML(`<a aria-label="Company, Foreign.">Foreign</a><section data-testid="job-description">`+fixtureText()+`</section>`))
		_, err := p.Capture(ctx, fixtureTarget(), limits)
		require.Equal(t, FailureIdentity, ErrorCode(err))
	})
	t.Run("collapsed content cannot read", func(t *testing.T) {
		p, _ := readerFixture(t, b, fixtureTarget(), fixtureHTML(`<section data-testid="job-description" data-collapsed="true">`+fixtureText()+`<button>Show more</button></section>`))
		s, err := p.Capture(ctx, fixtureTarget(), limits)
		require.NoError(t, err)
		_, err = p.Read(ctx, fixtureTarget(), s, fixturePlan(s), limits)
		require.Equal(t, FailureThin, ErrorCode(err))
	})
	t.Run("CSS clipped content cannot read", func(t *testing.T) {
		p, _ := readerFixture(t, b, fixtureTarget(), fixtureHTML(`<section data-testid="job-description" style="height:20px;overflow:hidden">`+fixtureText()+`</section>`))
		s, err := p.Capture(ctx, fixtureTarget(), limits)
		require.NoError(t, err)
		_, err = p.Read(ctx, fixtureTarget(), s, fixturePlan(s), limits)
		require.Equal(t, FailureThin, ErrorCode(err))
	})
	t.Run("snapshot text cap cannot pass as full content", func(t *testing.T) {
		p, _ := readerFixture(t, b, fixtureTarget(), fixtureHTML(`<section data-testid="job-description">`+fixtureText()+`</section>`))
		small := limits
		small.MaxTextChars = 120
		s, err := p.Capture(ctx, fixtureTarget(), small)
		require.NoError(t, err)
		_, err = p.Read(ctx, fixtureTarget(), s, fixturePlan(s), small)
		require.Equal(t, FailureThin, ErrorCode(err))
	})
	t.Run("identical DOM replacement invalidates current refs", func(t *testing.T) {
		p, page := readerFixture(t, b, fixtureTarget(), fixtureHTML(`<section data-testid="job-description">`+fixtureText()+`</section>`))
		s, err := p.Capture(ctx, fixtureTarget(), limits)
		require.NoError(t, err)
		_, err = page.Eval(`()=>{const n=document.querySelector('[data-testid="job-description"]');n.replaceWith(n.cloneNode(true))}`)
		require.NoError(t, err)
		_, err = p.Read(ctx, fixtureTarget(), s, fixturePlan(s), limits)
		require.Equal(t, FailureStale, ErrorCode(err))
	})
	t.Run("stale expansion and application controls rejected", func(t *testing.T) {
		p, page := readerFixture(t, b, fixtureTarget(), fixtureHTML(`<section data-testid="job-description">Short<button onclick="window.expanded=true">Show more</button></section>`))
		s, err := p.Capture(ctx, fixtureTarget(), limits)
		require.NoError(t, err)
		var expansion Action
		for _, a := range s.AllowedActions {
			if a.Kind == ActionExpand {
				expansion = a
				break
			}
		}
		require.Equal(t, ActionExpand, expansion.Kind)
		_, err = page.Eval(`()=>{const n=document.querySelector('[data-testid="job-description"]');n.replaceWith(n.cloneNode(true))}`)
		require.NoError(t, err)
		require.Equal(t, FailureStale, ErrorCode(p.Act(ctx, fixtureTarget(), s, expansion)))
		require.Equal(t, FailurePlan, ErrorCode(p.Act(ctx, fixtureTarget(), s, Action{Kind: ActionKind("submit"), Ref: expansion.Ref})))
		v, err := page.Eval(`()=>window.expanded===true||window.applied===true`)
		require.NoError(t, err)
		require.False(t, v.Value.Bool())
	})
	t.Run("navigation invalidates baseline and capture", func(t *testing.T) {
		p, page := readerFixture(t, b, fixtureTarget(), fixtureHTML(`<section data-testid="job-description">`+fixtureText()+`</section>`))
		require.NoError(t, page.Navigate("https://www.linkedin.com/jobs/view/999/"))
		require.NoError(t, page.WaitLoad())
		_, err := p.Known(ctx, fixtureTarget(), limits)
		require.Equal(t, FailureDrift, ErrorCode(err))
	})
}

func TestBrowserRecoveryBoundedAndScoped(t *testing.T) {
	b := readerFixtureBrowser(t)
	ctx := context.Background()
	limits := DefaultLimits()
	t.Run("owned expansion replaces description", func(t *testing.T) {
		html := fixtureHTML(`<section data-testid="job-description"><p>Short summary</p><button onclick="this.parentElement.outerHTML='<section data-testid=&quot;job-description&quot;>` + fixtureText() + `</section>'">Show more</button></section><button onclick="window.submitted=true">Submit application</button>`)
		p, page := readerFixture(t, b, fixtureTarget(), html)
		s, err := p.Capture(ctx, fixtureTarget(), limits)
		require.NoError(t, err)
		_, r, m, err := RecoverLocal(ctx, p, fixtureTarget(), s, limits)
		require.NoError(t, err)
		require.NotNil(t, r)
		require.Equal(t, 1, m.RecoveryActions)
		require.Equal(t, SourceRecovery, r.Source)
		v, err := page.Eval(`()=>window.submitted===true||window.applied===true`)
		require.NoError(t, err)
		require.False(t, v.Value.Bool())
	})
	t.Run("zero height lazy owned region hydrates", func(t *testing.T) {
		html := fixtureHTML(`<section id="JobDetails_AboutTheJob_123" style="margin-top:1500px"></section><div style="height:1000px"></div>`) + `<script>const n=document.getElementById('JobDetails_AboutTheJob_123');new IntersectionObserver(entries=>{if(entries.some(e=>e.isIntersecting))setTimeout(()=>n.textContent=` + fmt.Sprintf("%q", fixtureText()) + `,80)}).observe(n)</script>`
		p, _ := readerFixture(t, b, fixtureTarget(), html)
		s, err := p.Capture(ctx, fixtureTarget(), limits)
		require.NoError(t, err)
		_, r, m, err := RecoverLocal(ctx, p, fixtureTarget(), s, limits)
		require.NoError(t, err)
		require.NotNil(t, r)
		require.LessOrEqual(t, m.RecoveryActions, limits.MaxRecoveryActions)
	})
	t.Run("wait honors cancellation", func(t *testing.T) {
		p, _ := readerFixture(t, b, fixtureTarget(), fixtureHTML(`<section data-testid="job-description">Loading</section>`))
		s, err := p.Capture(ctx, fixtureTarget(), limits)
		require.NoError(t, err)
		deadline, cancel := context.WithTimeout(ctx, 80*time.Millisecond)
		defer cancel()
		start := time.Now()
		_, _, m, err := RecoverLocal(deadline, p, fixtureTarget(), s, limits)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Less(t, time.Since(start), 2*time.Second)
		require.LessOrEqual(t, m.RecoveryActions, limits.MaxRecoveryActions)
	})
	t.Run("custom limits preserved for actions", func(t *testing.T) {
		p, _ := readerFixture(t, b, fixtureTarget(), fixtureHTML(`<section data-testid="job-description">`+fixtureText()+`</section>`))
		custom := limits
		custom.MaxTextChars = 1234
		s, err := p.Capture(ctx, fixtureTarget(), custom)
		require.NoError(t, err)
		require.NoError(t, p.Act(ctx, fixtureTarget(), s, Action{Kind: ActionWait, Amount: 1}))
	})
	t.Run("missing content recovery stops at action budget", func(t *testing.T) {
		p, _ := readerFixture(t, b, fixtureTarget(), fixtureHTML(`<section data-testid="job-description">Loading</section>`))
		small := limits
		small.MaxRecoveryActions = 2
		s, err := p.Capture(ctx, fixtureTarget(), small)
		require.NoError(t, err)
		fresh, r, m, err := RecoverLocal(ctx, p, fixtureTarget(), s, small)
		require.NoError(t, err)
		require.Nil(t, r)
		require.Equal(t, 2, m.RecoveryActions)
		require.NotEmpty(t, fresh.ID)
	})
}

func TestBrowserScreenshotOwnedCrop(t *testing.T) {
	b := readerFixtureBrowser(t)
	ctx := context.Background()
	limits := DefaultLimits()
	for _, private := range []bool{false, true} {
		t.Run(fmt.Sprint(private), func(t *testing.T) {
			html := fixtureHTML(`<section data-testid="job-description" style="width:400px">` + fixtureText() + `</section>`)
			if private {
				html += `<aside style="position:fixed;inset:0;background:white">PRIVATE MESSAGING</aside>`
			}
			p, _ := readerFixture(t, b, fixtureTarget(), html)
			s, err := p.Capture(ctx, fixtureTarget(), limits)
			require.NoError(t, err)
			shot, err := p.ScreenshotFor(ctx, fixtureTarget(), s, limits)
			require.NoError(t, err)
			if private {
				require.Empty(t, shot)
			} else {
				require.NotEmpty(t, shot)
				require.LessOrEqual(t, len(shot), limits.MaxImageBytes)
			}
		})
	}
}
