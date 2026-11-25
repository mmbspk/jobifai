package jobpage

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestBrowserKnownPreservesExpiredAndHydrationBoundaries(t *testing.T) {
	b := readerFixtureBrowser(t)
	ctx := context.Background()
	limits := DefaultLimits()
	target := fixtureTarget()
	t.Run("expired baseline", func(t *testing.T) {
		p, _ := readerFixture(t, b, target, fixtureHTML("<div role=\"alert\">This job is no longer accepting applications.</div><section data-testid=\"job-description\">"+fixtureText()+"</section>"))
		p.baseline.Known = func(context.Context, Target, Limits) (Result, error) {
			return Result{Target: target, Identity: fixtureSnapshot().Identity, Description: fixtureText()}, nil
		}
		_, err := p.Known(ctx, target, limits)
		require.Equal(t, FailureExpired, ErrorCode(err))
	})
	t.Run("loading header", func(t *testing.T) {
		p, page := readerFixture(t, b, target, "<main><div role=\"progressbar\">Loading</div></main>")
		html := fixtureHTML("<section data-testid=\"job-description\">" + fixtureText() + "</section>")
		_, err := page.Eval("(html)=>{setTimeout(()=>{document.body.innerHTML=html},200)}", html)
		require.NoError(t, err)
		result, err := p.Known(ctx, target, limits)
		require.NoError(t, err)
		require.NotEmpty(t, result.Description)
	})
	t.Run("canonical seek alias", func(t *testing.T) {
		expected, _, err := NormalizeTarget(Target{URL: "https://www.seek.com.au/job/123", Title: "Engineer", Company: "Fixture"})
		require.NoError(t, err)
		current := expected
		current.URL = "https://au.seek.com/job/123"
		html := "<main data-job-id=\"123\"><h1>Engineer</h1><div data-automation=\"advertiser-name\">Fixture</div><section data-automation=\"jobAdDetails\">" + fixtureText() + "</section></main>"
		p, _ := readerFixture(t, b, current, html)
		result, err := p.Known(ctx, expected, limits)
		require.NoError(t, err)
		require.Equal(t, expected.JobID, result.Identity.JobID)
	})
 t.Run("company introduction inside owned description",func(t *testing.T){
  p,_:=readerFixture(t,b,target,fixtureHTML("<section data-testid=\"job-description\"><h2>About us</h2><p>We are the hiring team.</p><h2>Responsibilities</h2><p>"+fixtureText()+"</p></section>"))
  result,err:=p.Known(ctx,target,limits);require.NoError(t,err);require.Contains(t,result.Description,"Responsibilities");require.Contains(t,result.Description,"hiring team")
 })
}
