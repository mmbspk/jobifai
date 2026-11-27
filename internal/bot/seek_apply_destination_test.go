package bot

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llm"
)

func TestSeekApplyDestination_URLUsesOriginInsteadOfReferrerOrLoginText(t *testing.T) {
	for _, raw := range []string{
		"https://magentus.teamtailor.com/jobs/42/applications/new?referrer=https%3A%2F%2Fau.seek.com%2Fjob%2F12345678",
		"https://employer.example/login?from=seek.com",
		"https://seek.com.employer.example/job/12345678",
	} {
		require.Contains(t, seekApplyBlockedReasonFromURL(raw), "external site", raw)
	}
	for _, raw := range []string{
		"https://au.seek.com/job/12345678/apply?referrer=smartrecruiters.com",
		"https://www.seek.co.nz/job/12345678/apply",
		"https://login.seek.com/oauth",
	} {
		require.Empty(t, seekApplyBlockedReasonFromURL(raw), raw)
	}
}

func TestSeekApplyDestination_ExternalFormCannotReceiveInputOrBecomeCannotApply(t *testing.T) {
	database := openMappingDB(t)
	settings := config.NewStore(database)
	require.NoError(t, settings.Set(domain.SystemUserID, keyApplyEngineConfig, domain.ApplyEngineConfig{Mode: domain.ApplyEngineAdaptive}))
	b := New(Config{DB: database, UserID: "redirect-fixture", Platform: domain.PlatformSeek, Profile: basicProfile(), EngineStore: settings})
	b.cfg.Settings.HumanBehavior.PauseBetweenJobsMin = 1
	b.cfg.Settings.HumanBehavior.PauseBetweenJobsMax = 2
	const destination = "https://magentus.teamtailor.com/jobs/42/applications/new?referrer=https%3A%2F%2Fau.seek.com%2Fjob%2F12345678"
	browser := workerFixtureBrowser(t, func(raw string) string {
		if strings.Contains(raw, "teamtailor.com") {
			return "<html><body><h1>Choose documents</h1><form data-testid='quick-apply-form'><label>First name<input id='first'></label><input type='file' id='resume'><button type='button' onclick=\"localStorage.setItem('foreign-submit','true')\">Continue</button></form><script>document.addEventListener('input',()=>localStorage.setItem('foreign-input','true'));</script></body></html>"
		}
		if strings.Contains(raw, "/apply") {
			return "<html><body><script>location.replace('" + destination + "');</script></body></html>"
		}
		return seekWorkerHTML(raw)
	})
	job := seekJob{ID: "12345678", URL: "https://au.seek.com/job/12345678", Title: "Coordinator", Company: "Acme Inc.", EasyApply: true}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	lazy := &lazyDocGen{b: b, ctx: ctx, job: linkedInJob{ID: job.ID, URL: job.URL, Title: job.Title, Company: job.Company}}
	require.Equal(t, seekSubmitTopMatches, b.submitSeekApplication(ctx, browser, job, lazy, 8, "fixture", nil, llm.UsageSnapshot{}))
	var applied, pending int
	require.NoError(t, database.QueryRow("SELECT count(*) FROM jobs_applied WHERE user_id=?", b.cfg.UserID).Scan(&applied))
	require.Zero(t, applied)
	require.NoError(t, database.QueryRow("SELECT count(*) FROM jobs_pending_review WHERE user_id=? AND job_id=? AND easy_apply=0", b.cfg.UserID, job.ID).Scan(&pending))
	require.Equal(t, 1, pending)
	var outcome string
	require.NoError(t, database.QueryRow("SELECT outcome FROM apply_attempts WHERE user_id=? AND job_id=?", b.cfg.UserID, job.ID).Scan(&outcome))
	require.Equal(t, string(OutcomeBlocked), outcome)
	probe := navigateAndWait(t, browser, destination)
	state, err := probe.Eval("()=>({input:localStorage.getItem('foreign-input'),submit:localStorage.getItem('foreign-submit')})")
	require.NoError(t, err)
	require.True(t, state.Value.Get("input").Nil())
	require.True(t, state.Value.Get("submit").Nil())
}
