package jobpage

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBrowserRecoveryExpandsEmptyOwnedDescription(t *testing.T) {
	browser := readerFixtureBrowser(t)
	html := fixtureHTML(fmt.Sprintf("<section id='JobDetails_AboutTheJob_123'><div id='copy' style='display:none'>%s</div><button data-testid='expandable-text-button' aria-hidden='true' onclick=\"if(!event.isTrusted)return;window.expansionClicks=(window.expansionClicks||0)+1;document.getElementById('copy').style.display='block';this.remove()\">… more</button></section><button onclick='window.unexpectedApply=true'>Apply</button>", fixtureText()))
	page, raw := readerFixture(t, browser, fixtureTarget(), html)
	limits := DefaultLimits()
	limits.MaxRecoveryActions = 4
	snapshot, err := page.Capture(t.Context(), fixtureTarget(), limits)
	require.NoError(t, err)
	require.True(t, snapshot.Loading, "the empty owned region has no readable copy yet")
	require.False(t, snapshot.ActiveLoading, "absence alone does not prove an active load")
	_, result, metrics, err := RecoverLocal(t.Context(), page, fixtureTarget(), snapshot, limits)
	require.NoError(t, err)
	require.NotNil(t, result, "an empty collapsed description still needs its owned expansion")
	require.Equal(t, strings.TrimSpace(fixtureText()), result.Description)
	require.LessOrEqual(t, metrics.RecoveryActions, limits.MaxRecoveryActions)
	state, err := raw.Eval("()=>({clicks:window.expansionClicks||0,applied:window.unexpectedApply===true})")
	require.NoError(t, err)
	require.Equal(t, 1, state.Value.Get("clicks").Int())
	require.False(t, state.Value.Get("applied").Bool())
}
