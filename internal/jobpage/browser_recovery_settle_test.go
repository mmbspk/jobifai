package jobpage

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// An expansion can leave its enabled disclosure in place while the owned copy
// hydrates. Pressing it repeatedly can reset that work rather than advance it.
func TestBrowserRecoveryLetsBusyExpansionSettleWithinActionBudget(t *testing.T) {
	browser := readerFixtureBrowser(t)
	html := fixtureHTML(fmt.Sprintf(`<section id="JobDetails_AboutTheJob_123"><div id="copy" style="height:30px;overflow:hidden">%s</div><button data-testid="expandable-text-button" aria-hidden="true" onclick="if(!event.isTrusted)return;window.expansionClicks=(window.expansionClicks||0)+1;this.parentElement.setAttribute('aria-busy','true');clearTimeout(window.pendingExpansion);window.pendingExpansion=setTimeout(()=>{document.getElementById('copy').style.height='auto';this.parentElement.removeAttribute('aria-busy');this.remove()},900)">… more</button></section><button onclick="window.unexpectedApply=true">Apply</button>`, fixtureText()))
	page, raw := readerFixture(t, browser, fixtureTarget(), html)
	limits := DefaultLimits()
	limits.MaxRecoveryActions = 4
	snapshot, err := page.Capture(t.Context(), fixtureTarget(), limits)
	require.NoError(t, err)
	require.False(t, snapshot.Loading)
	_, result, metrics, err := RecoverLocal(t.Context(), page, fixtureTarget(), snapshot, limits)
	require.NoError(t, err)
	require.NotNil(t, result, "a busy expansion must settle rather than spend the entire budget on repeated presses")
	require.Equal(t, SourceRecovery, result.Source)
	require.Equal(t, strings.TrimSpace(fixtureText()), result.Description)
	require.LessOrEqual(t, metrics.RecoveryActions, limits.MaxRecoveryActions)
	state, err := raw.Eval(`()=>({clicks:window.expansionClicks,applied:window.unexpectedApply===true})`)
	require.NoError(t, err)
	require.Equal(t, 1, state.Value.Get("clicks").Int())
	require.False(t, state.Value.Get("applied").Bool())
}

func TestBrowserNativeExpansionRejectsHoverLoadingThenRecoversCurrentCopy(t *testing.T) {
	browser := readerFixtureBrowser(t)
	currentCopy := strings.Repeat("Hydrated current requirements: operate secure platforms and improve release reliability. ", 5)
	html := fixtureHTML(fmt.Sprintf(`
<section id="JobDetails_AboutTheJob_123" style="margin-top:900px">
  <div id="copy" style="height:30px;overflow:hidden">%s</div>
  <button data-testid="expandable-text-button" aria-hidden="true"
    onmouseover="this.parentElement.setAttribute('aria-busy','true');window.finishHydration=()=>{const copy=document.getElementById('copy');copy.textContent='%s';copy.style.height='auto';this.parentElement.removeAttribute('aria-busy');this.remove()}"
    onclick="window.expansionClicks=(window.expansionClicks||0)+1;clearTimeout(window.pendingExpansion)">… more</button>
</section>
<button onclick="window.unexpectedApply=true">Apply</button>`, fixtureText(), currentCopy))
	page, raw := readerFixture(t, browser, fixtureTarget(), html)
	limits := DefaultLimits()
	limits.MaxRecoveryActions = 4
	snapshot, err := page.Capture(t.Context(), fixtureTarget(), limits)
	require.NoError(t, err)
	require.False(t, snapshot.ActiveLoading)
	err = page.Act(t.Context(), fixtureTarget(), snapshot, firstExpansion(t, snapshot))
	require.Equal(t, FailureStale, ErrorCode(err), "hover-triggered loading must revoke the pending native press")
	state, err := raw.Eval(`()=>({clicks:window.expansionClicks||0,applied:window.unexpectedApply===true,busy:document.querySelector('[id^="JobDetails_AboutTheJob_"]').getAttribute('aria-busy')==='true'})`)
	require.NoError(t, err)
	require.True(t, state.Value.Get("busy").Bool())
	require.Zero(t, state.Value.Get("clicks").Int())
	require.False(t, state.Value.Get("applied").Bool())
	snapshot, err = page.Capture(t.Context(), fixtureTarget(), limits)
	require.NoError(t, err)
	require.True(t, snapshot.ActiveLoading)
	// Hold the fixture response until the boundary assertions finish, then
	// release it asynchronously to exercise recovery's bounded loading waits.
	_, err = raw.Eval(`()=>{window.pendingExpansion=setTimeout(window.finishHydration,900)}`)
	require.NoError(t, err)
	_, result, metrics, err := RecoverLocal(t.Context(), page, fixtureTarget(), snapshot, limits)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, SourceRecovery, result.Source)
	require.Equal(t, strings.TrimSpace(currentCopy), result.Description)
	require.LessOrEqual(t, metrics.RecoveryActions, limits.MaxRecoveryActions)
	state, err = raw.Eval(`()=>({clicks:window.expansionClicks||0,applied:window.unexpectedApply===true})`)
	require.NoError(t, err)
	require.Zero(t, state.Value.Get("clicks").Int())
	require.False(t, state.Value.Get("applied").Bool())
}

func TestBrowserReaderDoesNotPressBusyDisclosureOrInterpretLoadingCopy(t *testing.T) {
	browser := readerFixtureBrowser(t)
	html := fixtureHTML(fmt.Sprintf(`<section id="JobDetails_AboutTheJob_123" aria-busy="true"><div style="height:30px;overflow:hidden">%s</div><button data-testid="expandable-text-button" aria-hidden="true" onclick="window.expansionClicks=(window.expansionClicks||0)+1">… more</button></section><button onclick="window.unexpectedApply=true">Apply</button>`, fixtureText()))
	page, raw := readerFixture(t, browser, fixtureTarget(), html)
	modelSessions := 0
	reader := NewReader(nil, func(context.Context) (Config, error) {
		limits := DefaultLimits()
		limits.MaxRecoveryActions = 4
		return Config{Recovery: ModeOn, Limits: limits}, nil
	}, func(context.Context, Limits) (Model, error) {
		modelSessions++
		return nil, fmt.Errorf("loading content must never reach interpretation")
	})
	result, err := reader.Read(t.Context(), page, fixtureTarget())
	require.Equal(t, FailureThin, ErrorCode(err))
	require.Empty(t, result.Description)
	require.Equal(t, 4, result.Metrics.RecoveryActions)
	require.Zero(t, result.Metrics.HTTPAttempts)
	require.Zero(t, modelSessions)
	state, err := raw.Eval(`()=>({clicks:window.expansionClicks||0,applied:window.unexpectedApply===true})`)
	require.NoError(t, err)
	require.Zero(t, state.Value.Get("clicks").Int())
	require.False(t, state.Value.Get("applied").Bool())
}
