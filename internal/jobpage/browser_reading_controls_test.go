package jobpage

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBrowserLoadingIsScopedToReadingEvidence(t *testing.T) {
	browser := readerFixtureBrowser(t)
	for _, test := range []struct {
		name    string
		html    string
		loading bool
	}{
		{"unrelated sibling spinner", fixtureHTML(`<section data-testid="job-description">` + fixtureText() + `</section><div data-testid="salary-widget"><div role="progressbar">Loading salary details</div></div>`), false},
		{"hidden empty description template", fixtureHTML(`<section data-testid="job-description">` + fixtureText() + `</section><section data-testid="job-description" style="display:none"></section>`), false},
		{"accessibility-hidden description template", fixtureHTML(`<section data-testid="job-description">` + fixtureText() + `</section><section data-testid="job-description" aria-hidden="true">Loading...</section>`), false},
		{"description spinner", fixtureHTML(`<section data-testid="job-description">` + fixtureText() + `<div role="progressbar">Loading description</div></section>`), true},
		{"description ancestor busy", fixtureHTML(`<div aria-busy="true"><section data-testid="job-description">` + fixtureText() + `</section></div>`), true},
		{"description placeholder", fixtureHTML(`<section data-testid="job-description">Loading...</section><div role="progressbar">Loading salary details</div>`), true},
		{"empty description placeholder", fixtureHTML(`<section data-testid="job-description" style="min-height:20px"></section>`), true},
		{"header busy", strings.Replace(fixtureHTML(`<section data-testid="job-description">`+fixtureText()+`</section>`), `<h1>Engineer</h1>`, `<div aria-busy="true"><h1>Engineer</h1></div>`, 1), true},
		{"paragraph header busy", strings.Replace(fixtureHTML(`<section data-testid="job-description">`+fixtureText()+`</section>`), `<h1>Engineer</h1>`, `<div aria-busy="true"><p>Engineer</p></div>`, 1), true},
		{"scope busy", strings.Replace(fixtureHTML(`<section data-testid="job-description">`+fixtureText()+`</section>`), `<main data-job-id="123">`, `<main data-job-id="123" aria-busy="true">`, 1), true},
		{"unknown layout loading remains conservative", fixtureHTML(`<article>` + fixtureText() + `</article><div role="progressbar">Loading</div>`), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader, _ := readerFixture(t, browser, fixtureTarget(), test.html)
			snapshot, err := reader.Capture(t.Context(), fixtureTarget(), DefaultLimits())
			require.NoError(t, err)
			require.Equal(t, test.loading, snapshot.Loading)
			// Native status remains independent of adaptive node/text caps.
			reader.baseline.Known = func(context.Context, Target, Limits) (Result, error) {
				return Result{Identity: Identity{JobID: "123", Title: "Engineer", Company: "Acme", Verified: true}, Description: fixtureText()}, nil
			}
			limits := DefaultLimits()
			limits.MaxNodes = 1
			limits.MaxTextChars = 1
			result, err := reader.Known(t.Context(), fixtureTarget(), limits)
			if test.loading {
				require.Equal(t, FailureThin, ErrorCode(err))
			} else {
				require.NoError(t, err)
				require.Equal(t, SourceDeterministic, result.Source)
			}
		})
	}
}

func TestBrowserLinkedInPhysicalMoreControlDiscovery(t *testing.T) {
	browser := readerFixtureBrowser(t)
	for _, test := range []struct {
		name, control string
		platform      string
		allowed       bool
	}{
		{"rendered accessibility-hidden SDUI button", `<button data-testid="expandable-text-button" aria-hidden="true">… more</button>`, "linkedin", true},
		{"ordinary accessibility-hidden button", `<button aria-hidden="true">… more</button>`, "linkedin", false},
		{"same marker on SEEK cannot grant exception", `<button data-testid="expandable-text-button" aria-hidden="true">… more</button>`, "seek", false},
		{"hidden ancestor", `<div hidden><button data-testid="expandable-text-button" aria-hidden="true">… more</button></div>`, "linkedin", false},
		{"accessibility-hidden ancestor", `<div aria-hidden="true"><button data-testid="expandable-text-button" aria-hidden="true">… more</button></div>`, "linkedin", false},
		{"inert ancestor", `<div inert><button data-testid="expandable-text-button" aria-hidden="true">… more</button></div>`, "linkedin", false},
		{"inert control", `<button inert data-testid="expandable-text-button" aria-hidden="true">… more</button>`, "linkedin", false},
		{"aria disabled control", `<button aria-disabled="true" data-testid="expandable-text-button" aria-hidden="true">… more</button>`, "linkedin", false},
		{"disabled control", `<button disabled data-testid="expandable-text-button" aria-hidden="true">… more</button>`, "linkedin", false},
		{"display none", `<button style="display:none" data-testid="expandable-text-button" aria-hidden="true">… more</button>`, "linkedin", false},
		{"submit label cannot use marker", `<button data-testid="expandable-text-button" aria-hidden="true">Submit application</button>`, "linkedin", false},
		{"non-reading label cannot use marker", `<button data-testid="expandable-text-button" aria-hidden="true">Delete account</button>`, "linkedin", false},
		{"unmarked inert expansion", `<div inert><button>Show more</button></div>`, "linkedin", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			target := fixtureTarget()
			if test.platform == "seek" {
				target.Platform = "seek"
				target.URL = "https://www.seek.com.au/job/123"
			}
			reader, _ := readerFixture(t, browser, target, fixtureHTML(`<section data-testid="job-description"><p>`+fixtureText()+`</p><p aria-hidden="true">PRIVATE HIDDEN ANSWER</p>`+test.control+`</section>`))
			snapshot, err := reader.Capture(t.Context(), target, DefaultLimits())
			require.NoError(t, err)
			var expansions []Action
			for _, action := range snapshot.AllowedActions {
				if action.Kind == ActionExpand {
					expansions = append(expansions, action)
				}
			}
			if test.allowed {
				require.Len(t, expansions, 1)
				require.NoError(t, ValidateAction(snapshot, expansions[0]))
				node, _ := snapshot.Node(expansions[0].Ref)
				require.True(t, node.Visible)
			} else {
				require.Empty(t, expansions)
			}
			for _, node := range snapshot.Nodes {
				require.NotContains(t, node.Text, "PRIVATE HIDDEN")
			}
		})
	}
}

func TestBrowserExpansionElementModeRequiresReadingAction(t *testing.T) {
	browser := readerFixtureBrowser(t)
	reader, page := readerFixture(t, browser, fixtureTarget(), fixtureHTML(`<section data-testid="job-description">`+fixtureText()+`<button id="more" onclick="window.expanded=true">Show more</button></section>`))
	snapshot, err := reader.Capture(t.Context(), fixtureTarget(), DefaultLimits())
	require.NoError(t, err)
	var expand Action
	for _, action := range snapshot.AllowedActions {
		if action.Kind == ActionExpand {
			expand = action
			break
		}
	}
	require.NotEmpty(t, expand.Ref)
	opts, err := reader.options(t.Context(), fixtureTarget(), DefaultLimits())
	require.NoError(t, err)
	opts["mode"], opts["action"], opts["expected_epoch"] = "element", "expand", reader.epochs[snapshot.ID].material
	for _, test := range []struct {
		ref     string
		allowed bool
	}{{expand.Ref, true}, {snapshot.Candidates[0], false}, {snapshot.Identity.HeaderRefs[0], false}} {
		opts["ref"] = test.ref
		value, err := page.Eval(`(opts)=>{const el=(`+captureScript+`)(opts);return !!el&&el===document.getElementById('more');}`, opts)
		require.NoError(t, err)
		if test.allowed {
			require.True(t, value.Value.Bool())
		} else {
			value, err = page.Eval(`(opts)=>(`+captureScript+`)(opts)===null`, opts)
			require.NoError(t, err)
			require.True(t, value.Value.Bool())
		}
	}
	opts["mode"], opts["ref"] = "action", expand.Ref
	_, err = reader.eval(t.Context(), opts)
	require.Equal(t, FailurePlan, ErrorCode(err))
	value, err := page.Eval(`()=>window.expanded===true`)
	require.NoError(t, err)
	require.False(t, value.Value.Bool())
	// An expansion control replaced after capture must never be reacquired.
	_, err = page.Eval(`()=>document.getElementById('more').outerHTML=document.getElementById('more').outerHTML`)
	require.NoError(t, err)
	opts["mode"] = "element"
	_, err = reader.eval(t.Context(), opts)
	require.Equal(t, FailureStale, ErrorCode(err))
}

func TestBrowserRecoveryWaitsForPopulatedBusyDescription(t *testing.T) {
	browser := readerFixtureBrowser(t)
	reader, page := readerFixture(t, browser, fixtureTarget(), fixtureHTML(`<section data-testid="job-description" aria-busy="true">`+fixtureText()+`</section><article>`+fixtureText()+`</article>`))
	limits := DefaultLimits()
	limits.MaxRecoveryActions = 1
	snapshot, err := reader.Capture(t.Context(), fixtureTarget(), limits)
	require.NoError(t, err)
	require.True(t, snapshot.Loading)
	_, err = page.Eval(`()=>setTimeout(()=>document.querySelector('[aria-busy]').removeAttribute('aria-busy'),400)`)
	require.NoError(t, err)
	_, result, metrics, err := RecoverLocal(t.Context(), reader, fixtureTarget(), snapshot, limits)
	require.NoError(t, err)
	require.NotNil(t, result, "the action budget must give populated busy content time to finish loading")
	require.Equal(t, 1, metrics.RecoveryActions)
	require.Contains(t, result.Description, "Build secure services")
}
