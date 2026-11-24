package bot

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/documents"
	"github.com/user/jobifai/internal/domain"
)

func TestAdaptiveSeekApply_EmptyDocumentStepCannotAdvanceWithDocumentPolicies(t *testing.T) {
	db := openMappingDB(t)
	b := New(Config{DB: db, UserID: "fixture-user", Platform: domain.PlatformSeek, Documents: &documents.Service{}})
	br := workerFixtureBrowser(t, func(raw string) string {
		if strings.Contains(raw, "/apply") {
			return `<html><body><main><h1>Choose documents</h1><button data-testid="continue-button" onclick="window.advanced=true">Continue</button></main></body></html>`
		}
		return `<html><body><h1>Coordinator</h1><a data-automation="job-detail-company-name" href="https://au.seek.com/companies/acme">Acme</a><a data-automation="job-detail-apply" href="/job/12345678/apply">Quick Apply</a></body></html>`
	})
	job := linkedInJob{ID: "12345678", URL: "https://au.seek.com/job/12345678", Company: "Acme", Title: "Coordinator"}
	page := navigateAndWait(t, br, job.URL)
	ac := newAttemptContext(b.cfg.UserID, "seek", job.ID, job.URL, job.Title, job.Company, "fixture", "fixture-run")
	ac.Engine = domain.ApplyEngineAdaptive
	beginAttempt(db, ac)
	lazy := &lazyDocGen{b: b, ctx: context.Background(), job: job, attemptID: ac.AttemptID, frozen: true, packRestored: true}
	err := b.adaptiveSeekApply(context.Background(), page, lazy, ac)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "document step has no resolved controls")
	advanced, err := page.Eval(`()=>!!window.advanced`)
	require.NoError(t, err)
	assert.False(t, advanced.Value.Bool())
}

func TestSeekApplyWait_ClosedBrowserIsNotSessionExpiry(t *testing.T) {
	page := rodTestPage(t, `<html><body><p>Loading</p></body></html>`)
	require.NoError(t, page.Browser().Close())
	b := newTestBot(t)
	started := time.Now()
	found, blocked := b.seekWaitForApplyForm(page, time.Minute)
	assert.False(t, found)
	assert.Contains(t, blocked, "browser page unavailable")
	assert.NotContains(t, blocked, "session expired")
	assert.False(t, b.isSeekSessionExpired())
	assert.Less(t, time.Since(started), 5*time.Second)
}

func TestSeekAdaptiveNavigation_FullPageMainWithoutForm(t *testing.T) {
	html := `<html><body><header><label for="search">Search</label><input id="search"></header><main><h1>Your details</h1><progress value="1" max="2"></progress><div><label for="first">First name</label><input id="first"></div><div><button id="next" type="button" data-testid="continue-button">Con&#8288;tinue</button></div></main><aside><button onclick="window.wrong=true">Submit application</button></aside><script>document.getElementById('next').onclick=()=>{document.querySelector('main progress').value=2;document.querySelector('main h1').textContent='Questions';};</script></body></html>`
	br := workerFixtureBrowser(t, func(string) string { return html })
	page := navigateAndWait(t, br, "https://au.seek.com/job/12345678/apply")
	b := newTestBot(t)
	fields, _, err := b.scanAdaptiveStep(context.Background(), page)
	require.NoError(t, err)
	require.Len(t, fields, 1)
	assert.Equal(t, "First name", fields[0].Question)
	filler := &seekRodFiller{page: page, bot: b}
	_, err = page.Eval(`()=>{window.requestAnimationFrame=()=>0;}`)
	require.NoError(t, err)
	advanced, err := filler.NavigateNext(context.Background())
	require.NoError(t, err)
	assert.True(t, advanced)
	value, err := page.Eval(`()=>!!window.wrong`)
	require.NoError(t, err)
	assert.False(t, value.Value.Bool())
}

func TestSeekAdaptiveNavigation_HoverCannotChangeNextToUnguardedSubmit(t *testing.T) {
	br := workerFixtureBrowser(t, func(string) string {
		return `<html><body><main><h1>Your details</h1><button id="next" data-testid="continue-button">Continue</button></main><script>const b=document.getElementById('next');b.onmouseover=()=>{b.textContent='Submit application';};b.onclick=()=>{window.submits=(window.submits||0)+1;};</script></body></html>`
	})
	page := navigateAndWait(t, br, "https://au.seek.com/job/12345678/apply")
	advanced, err := (&seekRodFiller{page: page}).NavigateNext(context.Background())
	require.Error(t, err)
	assert.False(t, advanced)
	submits, err := page.Eval(`()=>window.submits||0`)
	require.NoError(t, err)
	assert.Zero(t, submits.Value.Int())
}

func TestSeekAdaptiveNavigation_HoverMovementOrReplacementDispatchesOnce(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		t.Run(map[bool]string{false: "movement", true: "replacement"}[replacement], func(t *testing.T) {
			mutation := `button.style.marginLeft='20px';`
			if replacement {
				mutation = `const fresh=button.cloneNode(true);fresh.onclick=advance;button.replaceWith(fresh);`
			}
			br := workerFixtureBrowser(t, func(string) string {
				return `<html><body><main><h1>Your details</h1><progress value=1 max=2></progress><button id=next data-testid=continue-button style="width:140px;height:44px">Continue</button></main><script>window.clicks=0;window.hovered=false;const button=document.getElementById('next');const advance=e=>{window.clicks++;window.trusted=e.isTrusted;document.querySelector('progress').value=2;};button.onclick=advance;button.onmouseover=()=>{if(window.hovered)return;window.hovered=true;` + mutation + `};</script></body></html>`
			})
			page := navigateAndWait(t, br, "https://au.seek.com/job/12345678/apply")
			advanced, err := (&seekRodFiller{page: page}).NavigateNext(context.Background())
			require.NoError(t, err)
			require.True(t, advanced)
			value, err := page.Eval(`()=>[window.clicks,window.trusted]`)
			require.NoError(t, err)
			require.Equal(t, 1, value.Value.Get("0").Int())
			require.True(t, value.Value.Get("1").Bool())
		})
	}
}

func TestSeekAdaptiveNavigation_SettlingCannotTurnNextIntoSubmit(t *testing.T) {
	br := workerFixtureBrowser(t, func(string) string {
		return `<html><body><main><h1>Your details</h1><button id=next data-testid=continue-button>Continue</button></main><script>window.clicks=0;document.getElementById('next').onclick=()=>window.clicks++;</script></body></html>`
	})
	page := navigateAndWait(t, br, "https://au.seek.com/job/12345678/apply")
	err := clickSeekAdaptiveAction(context.Background(), page, false, func(context.Context) error {
		_, err := page.Eval(`()=>{const button=document.getElementById('next');button.style.opacity='0';setTimeout(()=>{button.textContent='Submit application';button.style.opacity='1'},100);}`)
		return err
	})
	require.Error(t, err)
	value, err := page.Eval(`()=>window.clicks`)
	require.NoError(t, err)
	require.Zero(t, value.Value.Int())
}

func TestSeekAdaptiveNavigation_FullPageRequiresTrustedApplyRouteAndUniqueMain(t *testing.T) {
	for _, tc := range []struct{ name, url, extra string }{
		{"foreign host", "https://seek.com.au.example.com/job/12345678/apply", ""},
		{"listing page", "https://au.seek.com/job/12345678", ""},
		{"multiple mains", "https://au.seek.com/job/12345678/apply", `<main>Unrelated</main>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			br := workerFixtureBrowser(t, func(string) string {
				return `<html><body><main><input><button data-testid="continue-button">Continue</button></main>` + tc.extra + `</body></html>`
			})
			page := navigateAndWait(t, br, tc.url)
			fields := evalScanFields(t, page)
			assert.Empty(t, fields)
			button, submit, err := seekAdaptiveActionButton(page)
			require.Error(t, err)
			assert.Nil(t, button)
			assert.False(t, submit)
		})
	}
}

func TestSeekAdaptiveNavigation_DivApplicationWithoutProgressBar(t *testing.T) {
	for _, advance := range []bool{true, false} {
		t.Run(map[bool]string{true: "heading changes", false: "ignored click"}[advance], func(t *testing.T) {
			script := ""
			if advance {
				script = `<script>document.getElementById('next').onclick=()=>document.querySelector('#application h1').textContent='Employer questions';</script>`
			}
			br := workerFixtureBrowser(t, func(string) string {
				return `<html><body><header><input aria-label="Search"></header><div id="application"><div><h1>Choose documents</h1><label for="first">First name</label><input id="first"></div><div><div><button id="next" data-testid="continue-button">Continue</button></div></div></div><aside><button>Submit application</button></aside>` + script + `</body></html>`
			})
			page := navigateAndWait(t, br, "https://au.seek.com/job/12345678/apply")
			fields := evalScanFields(t, page)
			require.Len(t, fields, 1)
			assert.Equal(t, "First name", fields[0].Question)
			advanced, err := (&seekRodFiller{page: page}).NavigateNext(context.Background())
			assert.Equal(t, advance, advanced)
			if advance {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestSeekAdaptiveNavigation_DivApplicationRejectsSharedPageShell(t *testing.T) {
	for _, content := range []string{
		`<h1>Choose documents</h1><button data-testid="continue-button">Continue</button>`,
		`<div><header><input aria-label="Search"></header><h1>Choose documents</h1><button data-testid="continue-button">Continue</button></div>`,
		`<h1>Other heading</h1><div><h1>Choose documents</h1><button data-testid="continue-button">Continue</button></div>`,
	} {
		br := workerFixtureBrowser(t, func(string) string { return `<html><body>` + content + `</body></html>` })
		page := navigateAndWait(t, br, "https://au.seek.com/job/12345678/apply")
		button, submit, err := seekAdaptiveActionButton(page)
		require.Error(t, err)
		assert.Nil(t, button)
		assert.False(t, submit)
	}
}

func TestSeekAdaptiveNavigation_DivApplicationOwnsNonEditableHeaderAndProgressNav(t *testing.T) {
	br := workerFixtureBrowser(t, func(string) string {
		return `<html><body><div id="application"><header><h1>Apply for Coordinator</h1><nav><a href="#documents">Choose documents</a><a href="#questions">Questions</a></nav></header><section><label for="first">First name</label><input id="first"></section><div><button data-testid="continue-button">Continue</button></div></div><aside><button>Submit application</button></aside></body></html>`
	})
	page := navigateAndWait(t, br, "https://au.seek.com/job/12345678/apply")
	fields := evalScanFields(t, page)
	require.Len(t, fields, 1)
	assert.Equal(t, "First name", fields[0].Question)
	button, submit, err := seekAdaptiveActionButton(page)
	require.NoError(t, err)
	require.NotNil(t, button)
	assert.False(t, submit)
}

func TestSeekAdaptiveNavigation_NormalizesInvisibleCharactersAndScopesActions(t *testing.T) {
	page := rodTestPage(t, `<html><body><form data-testid="quick-apply-form"><button id="next" type="button">Con&#8288;tinue</button><button hidden>Submit application</button></form><aside><button>Submit application</button></aside></body></html>`)
	button, submit, err := seekAdaptiveActionButton(page)
	require.NoError(t, err)
	require.NotNil(t, button)
	assert.False(t, submit)
	id, err := button.Attribute("id")
	require.NoError(t, err)
	require.NotNil(t, id)
	assert.Equal(t, "next", *id)
}

func TestSeekAdaptiveNavigation_MissingScopeDoesNotExposeCandidateValues(t *testing.T) {
	page := rodTestPage(t, `<html><body><form><input value="private-candidate-value"><button type="button">Continue</button></form></body></html>`)
	button, submit, err := seekAdaptiveActionButton(page)
	require.Error(t, err)
	assert.Nil(t, button)
	assert.False(t, submit)
	assert.Contains(t, err.Error(), "navigation control missing")
	assert.NotContains(t, err.Error(), "private-candidate-value")
}
