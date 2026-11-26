package jobpage

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Nested SDUI wrappers must not turn every text/visibility check into another
// complete walk through the same ancestors. Count layout/style reads as well
// as imposing the reader deadline, so fast machines still catch regressions.
func TestBrowserCaptureNestedSDUITraversalIsBounded(t *testing.T) {
	browser := readerFixtureBrowser(t)
	var content strings.Builder
	content.WriteString(`<section id="JobDetails_AboutTheJob_123">`)
	for i := 0; i < 36; i++ {
		content.WriteString(strings.Repeat(`<div>`, 30))
		fmt.Fprintf(&content, `<p>Owned responsibility %d: Build secure services, own reliable releases, and collaborate with the engineering team.</p>`, i)
		content.WriteString(`<p hidden>HIDDEN PRIVATE TEXT</p><section data-job-id="999"><p>FOREIGN LISTING TEXT</p></section>`)
		content.WriteString(strings.Repeat(`</div>`, 30))
	}
	content.WriteString(`</section><aside><p>PRIVATE RECOMMENDATIONS</p></aside>`)
	reader, page := readerFixture(t, browser, fixtureTarget(), fixtureHTML(content.String()))
	_, err := page.Eval(`()=>{
	 const original=window.getComputedStyle;window.styleReadCount=0;
	 window.getComputedStyle=function(...args){window.styleReadCount++;return original.apply(this,args)};
	 window.fixtureElementCount=document.querySelectorAll('*').length;
	}`)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	started := time.Now()
	result, err := reader.Known(ctx, fixtureTarget(), DefaultLimits())
	require.NoError(t, err)
	require.Equal(t, SourceDeterministic, result.Source)
	require.Contains(t, result.Description, "Owned responsibility 0:")
	require.Contains(t, result.Description, "Owned responsibility 35:")
	require.NotContains(t, result.Description, "HIDDEN")
	require.NotContains(t, result.Description, "FOREIGN")
	require.NotContains(t, result.Description, "PRIVATE")
	counts, err := page.Eval(`()=>({styles:window.styleReadCount,elements:window.fixtureElementCount})`)
	require.NoError(t, err)
	styles, elements := counts.Value.Get("styles").Int(), counts.Value.Get("elements").Int()
	t.Logf("nested capture read in %s with %d style reads for %d elements", time.Since(started), styles, elements)
	require.LessOrEqual(t, styles, 12*elements, "style reads must remain proportional to the rendered DOM across fresh captures")
}

func TestBrowserCaptureMemoizationDoesNotSurviveDOMChanges(t *testing.T) {
	browser := readerFixtureBrowser(t)
	reader, page := readerFixture(t, browser, fixtureTarget(), fixtureHTML(`<section data-testid="job-description"><p id="copy">`+fixtureText()+`</p><p id="additional" hidden>`+fixtureText()+`Newly visible responsibility.</p></section>`))
	first, err := reader.Known(t.Context(), fixtureTarget(), DefaultLimits())
	require.NoError(t, err)
	require.NotContains(t, first.Description, "Newly visible")
	_, err = page.Eval(`()=>{document.getElementById('copy').hidden=true;document.getElementById('additional').hidden=false}`)
	require.NoError(t, err)
	second, err := reader.Known(t.Context(), fixtureTarget(), DefaultLimits())
	require.NoError(t, err)
	require.Contains(t, second.Description, "Newly visible responsibility.")
}
