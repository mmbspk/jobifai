package jobpage

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func nativeExpansionFixture(extra string) string {
	return fixtureHTML(fmt.Sprintf(`<section id="JobDetails_AboutTheJob_123" style="margin-top:900px"><div id="copy" style="height:30px;overflow:hidden">%s</div><button data-testid="expandable-text-button" aria-hidden="true" onclick="window.expansionClicks=(window.expansionClicks||0)+1;if(event.isTrusted){document.getElementById('copy').style.height='auto';this.remove()}">… more</button></section>%s`, fixtureText(), extra))
}

func firstExpansion(t *testing.T, s Snapshot) Action {
	t.Helper()
	for _, action := range s.AllowedActions {
		if action.Kind == ActionExpand {
			return action
		}
	}
	t.Fatal("the physically rendered owned expansion must be observed")
	return Action{}
}

func TestBrowserNativeExpansionUsesOneTrustedClickAndFreshProof(t *testing.T) {
	browser := readerFixtureBrowser(t)
	reader, page := readerFixture(t, browser, fixtureTarget(), nativeExpansionFixture(`<button onclick="window.applied=true">Submit application</button>`))
	snapshot, err := reader.Capture(t.Context(), fixtureTarget(), DefaultLimits())
	require.NoError(t, err)
	action := firstExpansion(t, snapshot)
	require.NoError(t, reader.Act(t.Context(), fixtureTarget(), snapshot, action))
	result, err := reader.Known(t.Context(), fixtureTarget(), DefaultLimits())
	require.NoError(t, err)
	require.Equal(t, fixtureText(), result.Description+" ")
	state, err := page.Eval(`()=>({clicks:window.expansionClicks,applied:window.applied===true})`)
	require.NoError(t, err)
	require.Equal(t, 1, state.Value.Get("clicks").Int())
	require.False(t, state.Value.Get("applied").Bool())
}

func TestBrowserNativeExpansionRejectsHoverReplacementOrActionChange(t *testing.T) {
	browser := readerFixtureBrowser(t)
	for _, test := range []struct{ name, hover string }{
		{"identical replacement", `this.replaceWith(this.cloneNode(true))`},
		{"application control", `this.textContent='Apply now';this.setAttribute('aria-label','Apply now')`},
		{"non-reading control with retained marker", `this.textContent='Delete account';this.setAttribute('aria-label','Delete account')`},
		{"listing drift", `document.querySelector('main').setAttribute('data-job-id','999')`},
	} {
		t.Run(test.name, func(t *testing.T) {
			html := nativeExpansionFixture("")
			html = strings.Replace(html, `aria-hidden="true"`, `aria-hidden="true" onmouseover="`+test.hover+`"`, 1)
			reader, page := readerFixture(t, browser, fixtureTarget(), html)
			snapshot, err := reader.Capture(t.Context(), fixtureTarget(), DefaultLimits())
			require.NoError(t, err)
			err = reader.Act(t.Context(), fixtureTarget(), snapshot, firstExpansion(t, snapshot))
			require.Error(t, err)
			state, err := page.Eval(`()=>({clicks:window.expansionClicks||0,applied:window.applied===true})`)
			require.NoError(t, err)
			require.Zero(t, state.Value.Get("clicks").Int())
			require.False(t, state.Value.Get("applied").Bool())
		})
	}
}

func TestBrowserNativeExpansionDoesNotClickOccludingControl(t *testing.T) {
	browser := readerFixtureBrowser(t)
	html := nativeExpansionFixture(`<button style="position:fixed;inset:0;z-index:999" onclick="window.applied=true">Apply now</button>`)
	reader, page := readerFixture(t, browser, fixtureTarget(), html)
	snapshot, err := reader.Capture(t.Context(), fixtureTarget(), DefaultLimits())
	require.NoError(t, err)
	err = reader.Act(t.Context(), fixtureTarget(), snapshot, firstExpansion(t, snapshot))
	require.Equal(t, FailureBrowser, ErrorCode(err))
	state, err := page.Eval(`()=>window.applied===true||window.expansionClicks>0`)
	require.NoError(t, err)
	require.False(t, state.Value.Bool())
}
