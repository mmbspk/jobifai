package bot

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llm"
)

// The owned disclosure represents page-local application readiness reached
// while reading the listing. Navigating again creates another unready page.
func evaluatedLinkedInPageFixture(id string) string {
	html := linkedInWorkerHTML(id, "Acme Inc.")
	html = strings.Replace(html, `<label for="notice">How soon can you leave?</label><input id="notice">`, "", 1)
	html = strings.Replace(html, `localStorage.setItem('lastNotice',document.getElementById('notice').value);`, "", 1)
	description := `<section class="jobs-description" data-job-description><div id="description" style="height:60px;overflow:hidden">` + strings.Repeat("Build and operate reliable production platforms, review code and support deployments. ", 8) + `</div><button id="show-more" data-testid="expandable-text-button">Show more</button></section>`
	html = strings.Replace(html, "</main>", description+"</main>", 1)
	return html + `<script>
window.applicationReady=false;
const openApplication=document.getElementById('cta').onclick;
document.getElementById('cta').onclick=event=>{
 if(!window.applicationReady){localStorage.setItem('unreadyApplicationClick','true');return;}
 openApplication(event);
};
document.getElementById('show-more').onclick=event=>{
 if(!event.isTrusted)return;
 const description=document.getElementById('description');description.style.height='auto';description.style.overflow='visible';
 window.applicationReady=true;localStorage.setItem('evaluatedDescription','true');event.currentTarget.remove();
};
</script>`
}

func TestProcessJob_ReusesEvaluatedPageThroughConfirmedApplication(t *testing.T) {
	db := openMappingDB(t)
	settings := config.NewStore(db)
	require.NoError(t, settings.Set(domain.SystemUserID, keyApplyEngineConfig, domain.ApplyEngineConfig{Mode: domain.ApplyEngineAdaptive}))
	scorer := &capturingScorer{}
	b := New(Config{DB: db, UserID: "evaluated-page", Platform: domain.PlatformLinkedIn, Profile: basicProfile(), EngineStore: settings, Scorer: scorer})
	job := linkedInJob{ID: "72345678", URL: "https://www.linkedin.com/jobs/view/72345678", Title: "Coordinator", Company: "Acme Inc."}
	var listingLoads atomic.Int32
	br := workerFixtureBrowser(t, func(raw string) string {
		if linkedInExpectedID(raw) == job.ID {
			listingLoads.Add(1)
		}
		return evaluatedLinkedInPageFixture(linkedInExpectedID(raw))
	})
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	require.True(t, b.processJob(ctx, br, job))
	require.Contains(t, scorer.gotDesc, "Build and operate reliable production platforms")
	require.EqualValues(t, 1, listingLoads.Load(), "application must retain the page read for suitability")
	// A second discovery cannot issue another application or navigation.
	require.False(t, b.processJob(ctx, br, job))
	require.EqualValues(t, 1, listingLoads.Load())
	var applied, confirmed int
	require.NoError(t, db.QueryRow("SELECT count(*) FROM jobs_applied WHERE user_id=? AND id=?", b.cfg.UserID, job.ID).Scan(&applied))
	require.Equal(t, 1, applied)
	require.NoError(t, db.QueryRow("SELECT count(*) FROM apply_attempts WHERE user_id=? AND job_id=? AND outcome='submitted_confirmed'", b.cfg.UserID, job.ID).Scan(&confirmed))
	require.Equal(t, 1, confirmed)
	pages, err := br.Pages()
	require.NoError(t, err)
	for _, page := range pages {
		info, err := page.Info()
		require.NoError(t, err)
		require.NotContains(t, info.URL, "/jobs/view/72345678", "processJob owns and closes the evaluated page")
	}
	probe := navigateAndWait(t, br, job.URL)
	value, err := probe.Eval(`()=>[localStorage.getItem('evaluatedDescription'),localStorage.getItem('unreadyApplicationClick'),localStorage.getItem('lastName'),localStorage.getItem('submits')]`)
	require.NoError(t, err)
	require.Equal(t, "true", value.Value.Get("0").String())
	require.True(t, value.Value.Get("1").Nil())
	require.Equal(t, b.cfg.Profile.PersonalInformation.Name, value.Value.Get("2").String())
	require.Equal(t, "1", value.Value.Get("3").String())
}

func TestSubmitEasyApply_CallerPageKeepsOwnershipAndRejectsWrongListing(t *testing.T) {
	db := openMappingDB(t)
	settings := config.NewStore(db)
	require.NoError(t, settings.Set(domain.SystemUserID, keyApplyEngineConfig, domain.ApplyEngineConfig{Mode: domain.ApplyEngineAdaptive}))
	b := New(Config{DB: db, UserID: "wrong-evaluated-page", Platform: domain.PlatformLinkedIn, Profile: basicProfile(), EngineStore: settings})
	br := workerFixtureBrowser(t, func(raw string) string { return linkedInWorkerHTML(linkedInExpectedID(raw), "Acme Inc.") })
	page := navigateAndWait(t, br, "https://www.linkedin.com/jobs/view/72345679")
	job := linkedInJob{ID: "72345678", URL: "https://www.linkedin.com/jobs/view/72345678", Title: "Coordinator", Company: "Acme Inc."}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	lazy := &lazyDocGen{b: b, ctx: ctx, job: job}
	require.False(t, b.submitEasyApplyOnPage(ctx, br, page, job, lazy, 8, "fixture", nil, llm.UsageSnapshot{}))
	info, err := page.Info()
	require.NoError(t, err, "the helper must leave a caller-owned page open")
	require.Contains(t, info.URL, "/jobs/view/72345679")
	value, err := page.Eval(`()=>[!!document.querySelector('.jobs-easy-apply-modal'),localStorage.getItem('submits')]`)
	require.NoError(t, err)
	require.False(t, value.Value.Get("0").Bool())
	require.True(t, value.Value.Get("1").Nil())
	var applied, dispatched int
	require.NoError(t, db.QueryRow("SELECT count(*) FROM jobs_applied WHERE user_id=?", b.cfg.UserID).Scan(&applied))
	require.Zero(t, applied)
	require.NoError(t, db.QueryRow("SELECT count(*) FROM apply_attempts WHERE user_id=? AND (stage='submit_dispatch' OR outcome='submitted_confirmed' OR outcome='submission_unknown')", b.cfg.UserID).Scan(&dispatched))
	require.Zero(t, dispatched)
}
