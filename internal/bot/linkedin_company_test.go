package bot

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
)

func TestLinkedInResolver_CompanyBadgesDoNotReplaceIdentityChecks(t *testing.T) {
	br := launchTestBrowser(t)
	for _, tc := range []struct {
		name, company, rootID string
		verified              bool
	}{
		{"missing company", "", "123", true},
		{"salary", "$190K/yr - $260K/yr", "123", true},
		{"salary without period", "$80K–$120K", "123", true},
		{"connection badge", "1 connection works here", "123", true},
		{"alumni badge", "11 company alumni work here", "123", true},
		{"different employer", "Unrelated Employer", "123", false},
		{"badge on a wrong job", "$190K/yr - $260K/yr", "999", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			html := `<main data-job-id="` + tc.rootID + `"><h1>Coordinator</h1><span aria-label="Company, Acme.">Acme</span><button onclick="window.clicks=(window.clicks||0)+1">Easy Apply</button><section data-job-id="888"><span aria-label="Company, Other.">Other</span><button>Easy Apply</button></section></main>`
			srv := fixtureServer(t, "123", "", html, "")
			page := navigateAndWait(t, br, srv.URL+"/jobs/view/123")
			r, err := resolveLinkedInJob(page, "123", true, linkedInJob{Title: "Coordinator", Company: tc.company})
			require.NoError(t, err)
			require.Equal(t, tc.verified, r.IdentityVerified)
			clicks, err := page.Eval(`()=>window.clicks||0`)
			require.NoError(t, err)
			if tc.verified {
				require.Equal(t, "Acme", r.Company)
				require.Equal(t, "clicked", r.Action)
				require.Equal(t, 1, clicks.Value.Int())
			} else {
				require.Empty(t, r.Action)
				require.Zero(t, clicks.Value.Int())
			}
		})
	}
}

func TestLinkedInResolver_StandaloneParagraphTitleAndIdentityGuard(t *testing.T) {
	br := launchTestBrowser(t)
	html := `<div><div id="JobDetails_ManageJobBanner_123"></div><div><div><p>Senior Coordinator</p><p>Adelaide</p><a aria-label="Company, Acme." href="/company/acme">Acme</a><a aria-label="Company, Acme." href="/company/acme">Logo</a><button onclick="window.clicks=(window.clicks||0)+1">Easy Apply</button></div></div><section><div id="JobDetails_AboutTheJob_123">Verified responsibilities for this listing.</div><aside><h2>Recommended role</h2><button>Easy Apply</button></aside></section></div>`
	srv := fixtureServer(t, "123", "", html, "")
	page := navigateAndWait(t, br, srv.URL+"/jobs/view/123")
	r, err := resolveLinkedInJob(page, "123", true, linkedInJob{Title: "Senior Coordinator", Company: "Acme"})
	require.NoError(t, err)
	require.True(t, r.IdentityVerified)
	require.Equal(t, "native", r.Kind)
	require.Equal(t, "clicked", r.Action)
	require.Contains(t, r.Description, "Verified responsibilities")
	require.NoError(t, verifyJobIdentity(page, "/jobs/view/123"), "pre-upload checks have no title metadata")
	_, err = page.Eval(`()=>{document.querySelector('[id^=JobDetails_ManageJobBanner_]').id='JobDetails_ManageJobBanner_999'}`)
	require.NoError(t, err)
	require.ErrorIs(t, verifyJobIdentity(page, "/jobs/view/123"), errLinkedInIdentity)
	clicks, err := page.Eval(`()=>window.clicks||0`)
	require.NoError(t, err)
	require.Equal(t, 1, clicks.Value.Int())
}

func TestReadLinkedInDescription_LazyControlIsExpandedOnce(t *testing.T) {
	br := launchTestBrowser(t)
	html := `<main data-job-id="123"><h1>Coordinator</h1><button>Easy Apply</button><div data-job-description style="min-height:40px;margin-top:1500px">Loading</div></main><script>
 const desc=document.querySelector('[data-job-description]');
 const observer=new IntersectionObserver(entries=>{if(!entries.some(e=>e.isIntersecting))return;observer.disconnect();setTimeout(()=>{
  const more=document.createElement('button');more.textContent='Show more';more.onclick=()=>{window.expands=(window.expands||0)+1;desc.firstChild.textContent='Verified complete responsibilities, qualifications and working arrangements. '.repeat(8);};desc.appendChild(more);
 },500);});observer.observe(desc);
 </script>`
	srv := fixtureServer(t, "123", "", html, "")
	page := navigateAndWait(t, br, srv.URL+"/jobs/view/123")
	r, err := readLinkedInDescription(context.Background(), page, linkedInJob{ID: "123", URL: srv.URL + "/jobs/view/123", Title: "Coordinator"})
	require.NoError(t, err)
	require.Greater(t, len(r.Description), 400)
	clicks, err := page.Eval(`()=>window.expands||0`)
	require.NoError(t, err)
	require.Equal(t, 1, clicks.Value.Int())
}

func TestLinkedInResolver_NativeClickIsTrustedAndRejectsHoverReplacement(t *testing.T) {
	br := launchTestBrowser(t)
	for _, replace := range []bool{false, true} {
		html := `<main data-job-id="123"><button onclick="if(event.isTrusted)window.trustedClicks=(window.trustedClicks||0)+1;else window.syntheticClicks=(window.syntheticClicks||0)+1">Easy Apply</button></main><script>requestAnimationFrame=()=>0;</script>`
		if replace {
			html += `<script>document.querySelector('button').onmouseover=()=>{document.querySelector('main').setAttribute('data-job-id','999');};</script>`
		}
		srv := fixtureServer(t, "123", "", html, "")
		page := navigateAndWait(t, br, srv.URL+"/jobs/view/123")
		r, err := resolveLinkedInJob(page, "123", true)
		counts, evalErr := page.Eval(`()=>({trusted:window.trustedClicks||0,synthetic:window.syntheticClicks||0})`)
		require.NoError(t, evalErr)
		require.Zero(t, counts.Value.Get("synthetic").Int())
		if replace {
			require.ErrorIs(t, err, errLinkedInIdentity)
			require.Zero(t, counts.Value.Get("trusted").Int())
			require.Empty(t, r.Action)
		} else {
			require.NoError(t, err)
			require.Equal(t, 1, counts.Value.Get("trusted").Int())
			require.Equal(t, "clicked", r.Action)
		}
	}
}

func TestLinkedInResolver_OwnedPseudoOverlayAndUnrelatedCover(t *testing.T) {
	br := launchTestBrowser(t)
	for _, covered := range []bool{false, true} {
		html := `<style>button{position:relative;width:160px;height:60px}button::after{content:'';position:absolute;inset:0;}</style><main data-job-id="123"><button onclick="if(event.isTrusted)window.clicks=(window.clicks||0)+1">Easy Apply</button></main>`
		if covered {
			html += `<div style="position:fixed;inset:0;z-index:1000"></div>`
		}
		srv := fixtureServer(t, "123", "", html, "")
		page := navigateAndWait(t, br, srv.URL+"/jobs/view/123")
		r, err := resolveLinkedInJob(page, "123", true)
		clicks, evalErr := page.Eval(`()=>window.clicks||0`)
		require.NoError(t, evalErr)
		if covered {
			require.Error(t, err)
			require.Empty(t, r.Action)
			require.Zero(t, clicks.Value.Int())
		} else {
			require.NoError(t, err)
			require.Equal(t, "clicked", r.Action)
			require.Equal(t, 1, clicks.Value.Int())
		}
	}
}

func TestLinkedInResolver_ReacquiresHydratedNativeControlAfterHover(t *testing.T) {
	br := launchTestBrowser(t)
	html := `<main data-job-id="123"><button id="old" onclick="window.oldClicks=(window.oldClicks||0)+1">Easy Apply</button></main><script>document.querySelector('button').onmouseover=()=>{document.querySelector('button').outerHTML='<button id="fresh" onclick="if(event.isTrusted)window.freshClicks=(window.freshClicks||0)+1">Easy Apply</button>';};</script>`
	srv := fixtureServer(t, "123", "", html, "")
	page := navigateAndWait(t, br, srv.URL+"/jobs/view/123")
	r, err := resolveLinkedInJob(page, "123", true)
	require.NoError(t, err)
	require.Equal(t, "clicked", r.Action)
	counts, err := page.Eval(`()=>({old:window.oldClicks||0,fresh:window.freshClicks||0})`)
	require.NoError(t, err)
	require.Zero(t, counts.Value.Get("old").Int())
	require.Equal(t, 1, counts.Value.Get("fresh").Int())
}

func TestProcessJob_RefreshesEmployerBeforeBlacklistAndScoring(t *testing.T) {
	br := launchTestBrowser(t)
	for _, company := range []string{"", "$190K/yr - $260K/yr"} {
		t.Run(company, func(t *testing.T) {
			html := `<main data-job-id="123"><h1>Coordinator</h1><span aria-label="Company, Acme.">Acme</span><div data-job-description>` + strings.Repeat("Relevant responsibilities and requirements. ", 8) + `</div><a href="https://employer.example/apply">Apply</a></main>`
			srv := fixtureServer(t, "123", "", html, "")
			db := testDB(t)
			scorer := &capturingScorer{}
			b := New(Config{DB: db, UserID: "u1", Platform: domain.PlatformLinkedIn, Scorer: scorer, Preferences: domain.WorkPreferences{CompanyBlacklist: []string{"Acme"}}})
			require.False(t, b.processJob(context.Background(), br, linkedInJob{ID: "123", URL: srv.URL + "/jobs/view/123", Title: "Coordinator", Company: company}))
			require.Empty(t, scorer.gotDesc, "blacklisted verified employer must not reach scoring")
			var recordedCompany, reason string
			require.NoError(t, db.QueryRow(`SELECT company,skip_reason FROM jobs_skipped WHERE id='123' AND user_id='u1'`).Scan(&recordedCompany, &reason))
			require.Equal(t, "Acme", recordedCompany)
			require.Equal(t, "blacklisted", reason)
		})
	}
}
