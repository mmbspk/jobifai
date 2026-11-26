package bot

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func nativeExpansionFixture(script string) string {
	return fmt.Sprintf(`<main data-job-id="123"><h1>Engineer</h1><a aria-label="Company, Acme.">Acme</a>
<button onclick="window.unexpectedApply=true">Easy Apply</button>
<section class="jobs-description-content__text"><p id="copy">%s</p>
<button id="expand" data-testid="expandable-text-button" aria-hidden="true" style="display:block;width:250px;height:30px">… more</button></section></main>
<script>window.nativeExpansions=0;const button=document.getElementById('expand');%s</script>`, strings.Repeat("Initial preview of current responsibilities. ", 4), script)
}

func TestLinkedInDescriptionExpansionUsesTrustedOwnedPointer(t *testing.T) {
	br := launchTestBrowser(t)
	full := strings.Repeat("Complete current requirements: Build secure services and collaborate with engineers. ", 8)
	script := fmt.Sprintf(`button.addEventListener('click',event=>{if(!event.isTrusted)return;window.nativeExpansions++;document.getElementById('copy').textContent=%q;button.remove();});`, full)
	srv := fixtureServer(t, "123", "", nativeExpansionFixture(script), "")
	page := navigateAndWait(t, br, srv.URL+"/jobs/view/123")
	job := linkedInJob{ID: "123", URL: srv.URL + "/jobs/view/123", Title: "Engineer", Company: "Acme"}
	result, err := readLinkedInDescription(t.Context(), page, job)
	require.NoError(t, err)
	require.True(t, result.IdentityVerified)
	require.Equal(t, full, result.Description+" ")
	state, err := page.Eval(`()=>({expansions:window.nativeExpansions,applied:window.unexpectedApply===true})`)
	require.NoError(t, err)
	require.Equal(t, 1, state.Value.Get("expansions").Int())
	require.False(t, state.Value.Get("applied").Bool())
}

func TestLinkedInDescriptionExpansionRechecksHoverReplacement(t *testing.T) {
	br := launchTestBrowser(t)
	script := `button.addEventListener('mouseover',()=>{button.outerHTML='<button id="replacement" data-testid="expandable-text-button" onclick="window.unexpectedApply=true">Apply now</button>';},{once:true});`
	srv := fixtureServer(t, "123", "", nativeExpansionFixture(script), "")
	page := navigateAndWait(t, br, srv.URL+"/jobs/view/123")
	job := linkedInJob{ID: "123", URL: srv.URL + "/jobs/view/123", Title: "Engineer", Company: "Acme"}
	_, err := evalLinkedInJob(page, "123", false, true, job)
	require.Error(t, err, "a replaced expansion control must be reacquired and verified before pressing")
	state, err := page.Eval(`()=>({replaced:!!document.getElementById('replacement'),applied:window.unexpectedApply===true})`)
	require.NoError(t, err)
	require.True(t, state.Value.Get("replaced").Bool())
	require.False(t, state.Value.Get("applied").Bool())
}

func TestLinkedInApplicationProbeStillRejectsAccessibilityHiddenControl(t *testing.T) {
	br := launchTestBrowser(t)
	srv := fixtureServer(t, "123", "", nativeExpansionFixture(""), "")
	page := navigateAndWait(t, br, srv.URL+"/jobs/view/123")
	button, err := page.Element("#expand")
	require.NoError(t, err)
	_, err = probeLinkedInNativeClickPoint(context.Background(), button)
	require.ErrorContains(t, err, "control_hidden")
}

func TestLinkedInDescriptionExpansionMarkerCannotAuthorizeNonReadingAction(t *testing.T) {
	br := launchTestBrowser(t)
	script := `button.addEventListener('mouseover',()=>{button.textContent='Delete account';button.setAttribute('aria-label','Delete account');},{once:true});button.addEventListener('click',()=>window.unexpectedApply=true);`
	srv := fixtureServer(t, "123", "", nativeExpansionFixture(script), "")
	page := navigateAndWait(t, br, srv.URL+"/jobs/view/123")
	job := linkedInJob{ID: "123", URL: srv.URL + "/jobs/view/123", Title: "Engineer", Company: "Acme"}
	_, err := evalLinkedInJob(page, "123", false, true, job)
	require.ErrorIs(t, err, errJobDescriptionIncomplete)
	state, err := page.Eval(`()=>({label:document.getElementById('expand').textContent,clicked:window.unexpectedApply===true})`)
	require.NoError(t, err)
	require.Equal(t, "Delete account", state.Value.Get("label").Str())
	require.False(t, state.Value.Get("clicked").Bool())
}

func TestLinkedInDescriptionExpansionReacquiresOwnedReplacement(t *testing.T) {
	br := launchTestBrowser(t)
	script := `button.addEventListener('mouseover',()=>{const fresh=button.cloneNode(true);button.replaceWith(fresh);fresh.addEventListener('click',event=>{if(event.isTrusted){window.nativeExpansions++;fresh.remove();}});},{once:true});button.addEventListener('click',()=>window.staleClicked=true);`
	srv := fixtureServer(t, "123", "", nativeExpansionFixture(script), "")
	page := navigateAndWait(t, br, srv.URL+"/jobs/view/123")
	job := linkedInJob{ID: "123", URL: srv.URL + "/jobs/view/123", Title: "Engineer", Company: "Acme"}
	result, err := evalLinkedInJob(page, "123", false, true, job)
	require.NoError(t, err)
	require.True(t, result.DescriptionExpanded)
	state, err := page.Eval(`()=>({expansions:window.nativeExpansions,stale:window.staleClicked===true,applied:window.unexpectedApply===true})`)
	require.NoError(t, err)
	require.Equal(t, 1, state.Value.Get("expansions").Int())
	require.False(t, state.Value.Get("stale").Bool())
	require.False(t, state.Value.Get("applied").Bool())
}

func TestLinkedInDescriptionExpansionRejectsHiddenAncestorDriftAndOverlay(t *testing.T) {
	br := launchTestBrowser(t)
	for _, test := range []struct {
		name   string
		script string
	}{
		{"hidden ancestor", `button.parentElement.setAttribute('aria-hidden','true');`},
		{"identity drift after hover", `button.addEventListener('mouseover',()=>document.querySelector('main').setAttribute('data-job-id','999'),{once:true});`},
		{"unrelated overlay after hover", `button.addEventListener('mouseover',()=>{const overlay=document.createElement('button');overlay.textContent='Apply now';overlay.style='position:fixed;inset:0;z-index:9999';overlay.onclick=()=>window.unexpectedApply=true;document.body.append(overlay);},{once:true});`},
	} {
		t.Run(test.name, func(t *testing.T) {
			srv := fixtureServer(t, "123", "", nativeExpansionFixture(test.script), "")
			page := navigateAndWait(t, br, srv.URL+"/jobs/view/123")
			job := linkedInJob{ID: "123", URL: srv.URL + "/jobs/view/123", Title: "Engineer", Company: "Acme"}
			result, err := evalLinkedInJob(page, "123", false, true, job)
			if test.name == "hidden ancestor" {
				require.NoError(t, err)
				require.True(t, result.DescriptionIncomplete, "hidden ancestry is rejected before any disclosure is discovered")
				require.False(t, result.DescriptionExpandable)
			} else {
				require.Error(t, err)
			}
			state, err := page.Eval(`()=>({expansions:window.nativeExpansions,applied:window.unexpectedApply===true})`)
			require.NoError(t, err)
			require.Zero(t, state.Value.Get("expansions").Int())
			require.False(t, state.Value.Get("applied").Bool())
		})
	}
}

func TestLinkedInDescriptionIgnoredExpansionCannotAcceptPreview(t *testing.T) {
	br := launchTestBrowser(t)
	srv := fixtureServer(t, "123", "", nativeExpansionFixture(""), "")
	page := navigateAndWait(t, br, srv.URL+"/jobs/view/123")
	job := linkedInJob{ID: "123", URL: srv.URL + "/jobs/view/123", Title: "Engineer", Company: "Acme"}
	result, err := readLinkedInDescription(t.Context(), page, job)
	require.ErrorIs(t, err, errJobDescriptionIncomplete)
	require.True(t, result.DescriptionIncomplete)
	require.Contains(t, err.Error(), "best=0 chars", "a preview cannot become saved complete evidence")
	state, err := page.Eval(`()=>window.unexpectedApply===true`)
	require.NoError(t, err)
	require.False(t, state.Value.Bool())
}

func TestLinkedInDescriptionClippedWithoutDisclosureCannotComplete(t *testing.T) {
	br := launchTestBrowser(t)
	script := `button.remove();document.querySelector('section').style='height:20px;overflow:hidden';`
	srv := fixtureServer(t, "123", "", nativeExpansionFixture(script), "")
	page := navigateAndWait(t, br, srv.URL+"/jobs/view/123")
	job := linkedInJob{ID: "123", URL: srv.URL + "/jobs/view/123", Title: "Engineer", Company: "Acme"}
	result, err := evalLinkedInJob(page, "123", false, false, job)
	require.NoError(t, err)
	require.True(t, result.IdentityVerified)
	require.False(t, result.DescriptionExpandable)
	require.True(t, result.DescriptionIncomplete)
}

func TestLinkedInDescriptionRetainsOnlyCompleteEvidenceThroughRerender(t *testing.T) {
	br := launchTestBrowser(t)
	full := strings.Repeat("Complete verified requirements for the current engineer vacancy. ", 8)
	script := fmt.Sprintf(`button.remove();document.getElementById('copy').textContent=%q;`, full)
	srv := fixtureServer(t, "123", "", nativeExpansionFixture(script), "")
	page := navigateAndWait(t, br, srv.URL+"/jobs/view/123")
	_, err := page.Eval(`()=>{const desc=document.querySelector('section');setTimeout(()=>desc.style.display='none',150);setTimeout(()=>{desc.style.display='block';document.getElementById('copy').textContent='Current complete rendered requirements. '.repeat(4)},650)}`)
	require.NoError(t, err)
	job := linkedInJob{ID: "123", URL: srv.URL + "/jobs/view/123", Title: "Engineer", Company: "Acme"}
	result, err := readLinkedInDescription(t.Context(), page, job)
	require.NoError(t, err)
	require.False(t, result.DescriptionIncomplete)
	require.Equal(t, full, result.Description+" ")
}
