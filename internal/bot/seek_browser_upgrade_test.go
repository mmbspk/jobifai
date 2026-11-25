package bot

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/browser"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/domain"
)

const seekCompatibilityFixture = `<!doctype html><html><body>
<h1>Browser Upgrade</h1><span role="button" tabindex="0" style="display:inline-block;padding:20px;margin-top:1200px" onclick="document.cookie='UpdateBrowserMessage=1; path=/'; localStorage.setItem('ackTrusted',String(event.isTrusted)); location.href='/';">Continue and remind me later</span>
</body></html>`

func TestSeekBrowserUpgrade_RecoversAndKeepsAcknowledgementAcrossTabs(t *testing.T) {
	br := workerFixtureBrowser(t, func(raw string) string {
		if strings.Contains(raw, "/browser-upgrade") {
			return seekCompatibilityFixture
		}
		return "<h1>Engineer</h1><main>Full intended job description</main>"
	})
	jobURL := "https://au.seek.com/job/95969588"
	page := navigateAndWait(t, br, "https://au.seek.com/browser-upgrade?continue="+url.QueryEscape(jobURL))
	require.ErrorIs(t, browser.CheckSeekBrowser(page), browser.ErrSeekBrowserUnsupported)
	require.NoError(t, browser.RecoverSeekBrowserUpgrade(page, jobURL))
	info, err := page.Info()
	require.NoError(t, err)
	require.Equal(t, jobURL, info.URL)
	value, err := page.Eval(`()=>[document.cookie,localStorage.getItem('ackTrusted'),document.querySelector('h1').textContent]`)
	require.NoError(t, err)
	require.Contains(t, value.Value.Get("0").String(), "UpdateBrowserMessage=1")
	require.Equal(t, "true", value.Value.Get("1").String(), "acknowledge through SEEK's real control")
	require.Equal(t, "Engineer", value.Value.Get("2").String())
	other := navigateAndWait(t, br, jobURL)
	value, err = other.Eval(`()=>document.cookie`)
	require.NoError(t, err)
	require.Contains(t, value.Value.String(), "UpdateBrowserMessage=1")
}

func TestSeekBrowserUpgrade_RejectsExternalContinueWithoutClicking(t *testing.T) {
	br := workerFixtureBrowser(t, func(string) string { return seekCompatibilityFixture })
	page := navigateAndWait(t, br, "https://au.seek.com/browser-upgrade?continue=https%3A%2F%2Fexample.org%2F")
	require.ErrorIs(t, browser.RecoverSeekBrowserUpgrade(page, "https://au.seek.com/job/123"), browser.ErrSeekBrowserUnsupported)
	value, err := page.Eval(`()=>document.cookie`)
	require.NoError(t, err)
	require.NotContains(t, value.Value.String(), "UpdateBrowserMessage")
}

func TestSeekBrowserUpgrade_NoContinueRemainsBlocked(t *testing.T) {
	br := workerFixtureBrowser(t, func(string) string { return "<h1>Browser Upgrade</h1>" })
	page := navigateAndWait(t, br, "https://au.seek.com/browser-upgrade")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.ErrorIs(t, browser.RecoverSeekBrowserUpgrade(page.Context(ctx), "https://au.seek.com/job/123"), browser.ErrSeekBrowserUnsupported)
}

type upgradeScorer struct{ calls int }

func (s *upgradeScorer) EvaluateJob(context.Context, *domain.ResumeProfile, string) (domain.JobScore, error) {
	s.calls++
	return domain.JobScore{Score: 0}, nil
}

func TestSeekBrowserUpgrade_DirectApplyNeverScoresCompatibilityPage(t *testing.T) {
	br, endpoint := launchTestBrowserEndpoint(t)
	router := br.HijackRequests()
	router.MustAdd("*", func(h *rod.Hijack) {
		h.Response.SetHeader("Content-Type", "text/html")
		// The real unsupported route is loaded as a server redirect would load it.
		if strings.Contains(h.Request.URL().Path, "/job/") {
			h.Response.SetBody("<script>location.href='/browser-upgrade'</script>")
		} else {
			h.Response.SetBody("<h1>Browser Upgrade</h1>")
		}
	})
	go router.Run()
	t.Cleanup(func() { router.MustStop() })
	u, err := url.Parse(endpoint)
	require.NoError(t, err)
	port, err := strconv.Atoi(u.Port())
	require.NoError(t, err)
	db := openMappingDB(t)
	cfg := config.NewStore(db)
	secrets := config.NewSecretsStore(db, "fixture")
	gs := domain.GeneralSettings{}
	gs.Browser.RemoteDebugPort = port
	require.NoError(t, cfg.Set(domain.SystemUserID, "general_settings", gs))
	require.NoError(t, cfg.Set("fixture-user", "resume_profile", basicProfile()))
	scorer := &upgradeScorer{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	m := NewManager(ctx, db, cfg, secrets, browser.NewSessionStore(db, secrets), nil, scorer, nil, nil, "")
	_, err = m.ApplyFromURL(ctx, "fixture-user", "https://au.seek.com/job/95969588", "", false)
	require.ErrorIs(t, err, browser.ErrSeekBrowserUnsupported)
	require.Zero(t, scorer.calls)
	var outcome, stage, code string
	require.NoError(t, db.QueryRow("SELECT outcome,stage,reason_code FROM apply_attempts WHERE user_id='fixture-user' ORDER BY started_at DESC LIMIT 1").Scan(&outcome, &stage, &code))
	require.Equal(t, "blocked", outcome)
	require.Equal(t, "browser", stage)
	require.Equal(t, "seek_browser_unsupported", code)
}
