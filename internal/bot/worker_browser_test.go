package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/go-rod/rod"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llm"
)

func workerFixtureBrowser(t *testing.T, dispatch func(string) string) *rod.Browser {
	t.Helper()
	br := launchTestBrowser(t)
	router := br.HijackRequests()
	router.MustAdd("*", func(request *rod.Hijack) {
		request.Response.SetHeader("Content-Type", "text/html; charset=utf-8")
		request.Response.SetBody(dispatch(request.Request.URL().String()))
	})
	go router.Run()
	t.Cleanup(func() { router.MustStop() })
	return br
}

func linkedInWorkerHTML(id, confirmationCompany string) string {
	return fmt.Sprintf(`<!DOCTYPE html><html><body><main data-job-id="%s"><h1>Coordinator at Acme Inc.</h1><div class="job-details-jobs-unified-top-card__company-name"><a href="https://www.linkedin.com/company/acme-inc">Acme Inc.</a></div><button id="cta">Easy Apply</button></main>
 <script>document.getElementById('cta').onclick=()=>{
 const modal=document.createElement('div');modal.className='jobs-easy-apply-modal';modal.setAttribute('role','dialog');modal.setAttribute('aria-label','Easy Apply');
 modal.innerHTML='<h2>Apply to Acme Inc.</h2><progress value="1" max="2"></progress><div id="controls"><label for="name">First name</label><input id="name"><label for="notice">How soon can you leave?</label><input id="notice"></div><button id="next" data-live-test-easy-apply-next-button>Next</button>';
 document.body.appendChild(modal);
 const next=modal.querySelector('button');next.onclick=()=>{
 localStorage.setItem('lastName',document.getElementById('name').value);localStorage.setItem('lastNotice',document.getElementById('notice').value);
 document.getElementById('controls').innerHTML='';modal.querySelector('progress').value=2;next.removeAttribute('data-live-test-easy-apply-next-button');next.setAttribute('data-live-test-easy-apply-submit-button','');next.textContent='Submit application';
 next.onclick=()=>{localStorage.setItem('submits',String(Number(localStorage.getItem('submits')||0)+1));modal.textContent='Your application was sent to %s';};
 };};</script></body></html>`, id, confirmationCompany)
}

func TestWorkerBrowser_LinkedInColdWarmThroughHistory(t *testing.T) {
	ctx := context.Background()
	db := openMappingDB(t)
	settings := config.NewStore(db)
	require.NoError(t, settings.Set(domain.SystemUserID, keyApplyEngineConfig, domain.ApplyEngineConfig{Mode: domain.ApplyEngineAdaptive}))
	srv := newCountingServer(t, func([]byte) string {
		return `[{"ref":"f1","action":"fill","intent_type":"profile","binding_key":"application_defaults.notice_period"}]`
	})
	mappings := &DBMappingStore{DB: db, UserID: "fixture-user"}
	retained := 0
	b := New(Config{DB: db, UserID: "fixture-user", Platform: domain.PlatformLinkedIn, Profile: basicProfile(), EngineStore: settings, AdaptiveMapping: mappings, AdaptiveLLM: NewLLMFormInterpreter(srv.newClient(t)), OnApplied: func() { retained++ }})
	br := workerFixtureBrowser(t, func(raw string) string { return linkedInWorkerHTML(linkedInExpectedID(raw), "Acme Inc.") })
	for _, id := range []string{"12345678", "12345679"} {
		job := linkedInJob{ID: id, URL: "https://www.linkedin.com/jobs/view/" + id, Title: "Coordinator", Company: "Acme Inc."}
		lazy := &lazyDocGen{b: b, ctx: ctx, job: job}
		require.True(t, b.submitEasyApply(ctx, br, job, lazy, 8, "fixture", nil, llm.UsageSnapshot{}))
		if id == "12345678" {
			var mappingID string
			require.NoError(t, db.QueryRow(`SELECT id FROM form_field_mappings WHERE user_id=? AND binding_key='application_defaults.notice_period'`, b.cfg.UserID).Scan(&mappingID))
			require.NoError(t, ConfirmFormMapping(ctx, db, b.cfg.UserID, mappingID, "application_defaults.notice_period", basicProfile()))
			b.cfg.AdaptiveLLM = nil
		}
	}
	assert.Equal(t, int32(1), srv.requests.Load())
	assert.Equal(t, 2, retained)
	var count int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM jobs_applied WHERE user_id=?`, b.cfg.UserID).Scan(&count))
	assert.Equal(t, 2, count)
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM apply_attempts WHERE user_id=? AND engine='adaptive' AND outcome='submitted_confirmed'`, b.cfg.UserID).Scan(&count))
	assert.Equal(t, 2, count)
	probe := navigateAndWait(t, br, "https://www.linkedin.com/jobs/view/12345679")
	value, err := probe.Eval(`()=>[localStorage.getItem('lastName'),localStorage.getItem('lastNotice'),localStorage.getItem('submits')].join('|')`)
	require.NoError(t, err)
	assert.Equal(t, "Fatima|2 weeks|2", value.Value.String())
}

func seekWorkerHTML(raw string) string {
	id := seekJobIDFromURL(raw)
	if strings.Contains(raw, "/apply") {
		return `<html><body><form data-testid="quick-apply-form"><h1>Your details</h1><progress value="1" max="2"></progress><div id="controls"><label for="name">First name</label><input id="name"><label for="notice">How soon can you leave?</label><input id="notice"></div><button id="next" type="button" data-testid="continue-button">Continue</button></form><aside><button onclick="window.wrongSubmit=true">Submit</button></aside><script>
  const form=document.querySelector('form'),next=document.getElementById('next');next.onclick=()=>{localStorage.setItem('lastNotice',document.getElementById('notice').value);document.getElementById('controls').innerHTML='';form.querySelector('progress').value=2;next.setAttribute('data-testid','review-submit-application');next.textContent='Submit application';next.onclick=()=>{localStorage.setItem('applied-` + id + `','true');localStorage.setItem('submits',String(Number(localStorage.getItem('submits')||0)+1));form.innerHTML='<h1 data-automation="application-success-title">Application submitted</h1>';};};
  </script></body></html>`
	}
	return `<html><body><h1>Coordinator</h1><a data-automation="job-detail-company-name" href="https://au.seek.com/companies/acme-inc">Acme Inc.</a><a id="apply" data-automation="job-detail-apply" href="/job/` + id + `/apply">Quick Apply</a><script>if(localStorage.getItem('applied-` + id + `'))document.getElementById('apply').textContent='Applied';</script></body></html>`
}

func TestWorkerBrowser_SeekColdWarmThroughHistory(t *testing.T) {
	ctx := context.Background()
	db := openMappingDB(t)
	settings := config.NewStore(db)
	require.NoError(t, settings.Set(domain.SystemUserID, keyApplyEngineConfig, domain.ApplyEngineConfig{Mode: domain.ApplyEngineAdaptive}))
	srv := newCountingServer(t, func([]byte) string {
		return `[{"ref":"f1","action":"fill","intent_type":"profile","binding_key":"application_defaults.notice_period"}]`
	})
	mappings := &DBMappingStore{DB: db, UserID: "seek-fixture"}
	b := New(Config{DB: db, UserID: "seek-fixture", Platform: domain.PlatformSeek, Profile: basicProfile(), EngineStore: settings, AdaptiveMapping: mappings, AdaptiveLLM: NewLLMFormInterpreter(srv.newClient(t))})
	b.cfg.Settings.HumanBehavior.PauseBetweenJobsMin = 1
	b.cfg.Settings.HumanBehavior.PauseBetweenJobsMax = 2
	br := workerFixtureBrowser(t, seekWorkerHTML)
	for _, id := range []string{"22345678", "22345679"} {
		job := seekJob{ID: id, URL: "https://au.seek.com/job/" + id, Title: "Coordinator", Company: "Acme Inc.", EasyApply: true}
		lazy := &lazyDocGen{b: b, ctx: ctx, job: linkedInJob{ID: id, URL: job.URL, Title: job.Title, Company: job.Company}}
		require.Equal(t, seekSubmitApplied, b.submitSeekApplication(ctx, br, job, lazy, 8, "fixture", nil, llm.UsageSnapshot{}))
		if id == "22345678" {
			var mappingID string
			require.NoError(t, db.QueryRow(`SELECT id FROM form_field_mappings WHERE user_id=? AND binding_key='application_defaults.notice_period'`, b.cfg.UserID).Scan(&mappingID))
			require.NoError(t, ConfirmFormMapping(ctx, db, b.cfg.UserID, mappingID, "application_defaults.notice_period", basicProfile()))
			b.cfg.AdaptiveLLM = nil
		}
	}
	assert.Equal(t, int32(1), srv.requests.Load())
	var count int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM jobs_applied WHERE user_id=?`, b.cfg.UserID).Scan(&count))
	assert.Equal(t, 2, count)
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM apply_attempts WHERE user_id=? AND outcome='submitted_confirmed' AND engine='adaptive'`, b.cfg.UserID).Scan(&count))
	assert.Equal(t, 2, count)
}

func TestWorkerBrowser_GreenhouseHandoffFromBothPlatforms(t *testing.T) {
	for _, platform := range []domain.Platform{domain.PlatformLinkedIn, domain.PlatformSeek} {
		t.Run(string(platform), func(t *testing.T) {
			db := openMappingDB(t)
			settings := config.NewStore(db)
			require.NoError(t, settings.Set(domain.SystemUserID, keyApplyEngineConfig, domain.ApplyEngineConfig{Mode: domain.ApplyEngineAdaptive, ExternalATSEnabled: true}))
			b := New(Config{DB: db, UserID: "handoff", Platform: platform, Profile: basicProfile(), EngineStore: settings, EnableExternalATS: true})
			const target = "https://boards.greenhouse.io/acmeinc/jobs/4567890"
			br := workerFixtureBrowser(t, func(raw string) string {
				if classifyExternalATS(raw) == ATSKindGreenhouse {
					return `<html><body><form id="application_form"><label for="email">Email address</label><input id="email" type="email" required><button type="button">Submit application</button></form><script>document.querySelector('button').onclick=()=>{localStorage.setItem('email',document.getElementById('email').value);document.querySelector('form').outerHTML='<div id="application_confirmation"><h1>Application received</h1></div>';};</script></body></html>`
				}
				if platform == domain.PlatformLinkedIn {
					return `<html><body><main data-job-id="32345678"><h1>Coordinator at Acme Inc.</h1><a href="` + target + `">Apply</a></main></body></html>`
				}
				return `<html><body><h1>Coordinator</h1><span data-automation="job-detail-company-name">Acme Inc.</span><a data-automation="job-detail-apply" href="` + target + `">Apply</a></body></html>`
			})
			job := linkedInJob{ID: "32345678", Title: "Coordinator", Company: "Acme Inc."}
			if platform == domain.PlatformLinkedIn {
				job.URL = "https://www.linkedin.com/jobs/view/32345678"
			} else {
				job.URL = "https://au.seek.com/job/32345678"
			}
			lazy := &lazyDocGen{b: b, ctx: context.Background(), job: job}
			if platform == domain.PlatformLinkedIn {
				require.True(t, b.submitEasyApply(context.Background(), br, job, lazy, 8, "fixture", nil, llm.UsageSnapshot{}))
			} else {
				require.Equal(t, seekSubmitApplied, b.submitSeekApplication(context.Background(), br, seekJob{ID: job.ID, URL: job.URL, Title: job.Title, Company: job.Company}, lazy, 8, "fixture", nil, llm.UsageSnapshot{}))
			}
			var count int
			require.NoError(t, db.QueryRow(`SELECT count(*) FROM jobs_applied WHERE user_id=? AND link=?`, b.cfg.UserID, job.URL).Scan(&count))
			assert.Equal(t, 1, count)
			var raw, outcome string
			require.NoError(t, db.QueryRow(`SELECT external_identity_json,outcome FROM apply_attempts WHERE user_id=?`, b.cfg.UserID).Scan(&raw, &outcome))
			assert.Equal(t, string(OutcomeSubmittedConfirmed), outcome)
			var identity map[string]string
			require.NoError(t, json.Unmarshal([]byte(raw), &identity))
			assert.Equal(t, "greenhouse", identity["platform"])
			assert.Equal(t, "acmeinc", identity["tenant"])
			assert.Equal(t, "4567890", identity["job_id"])
		})
	}
}

func TestWorkerBrowser_WrongEmployerConfirmationCannotRecordOrResubmit(t *testing.T) {
	db := openMappingDB(t)
	settings := config.NewStore(db)
	require.NoError(t, settings.Set(domain.SystemUserID, keyApplyEngineConfig, domain.ApplyEngineConfig{Mode: domain.ApplyEngineAdaptive}))
	srv := newCountingServer(t, func([]byte) string {
		return `[{"ref":"f1","action":"fill","intent_type":"profile","binding_key":"application_defaults.notice_period"}]`
	})
	b := New(Config{DB: db, UserID: "false-confirmation", Platform: domain.PlatformLinkedIn, Profile: basicProfile(), EngineStore: settings, AdaptiveLLM: NewLLMFormInterpreter(srv.newClient(t))})
	br := workerFixtureBrowser(t, func(raw string) string { return linkedInWorkerHTML(linkedInExpectedID(raw), "Unrelated Employer") })
	job := linkedInJob{ID: "42345678", URL: "https://www.linkedin.com/jobs/view/42345678", Title: "Coordinator", Company: "Acme Inc."}
	for i := 0; i < 2; i++ {
		lazy := &lazyDocGen{b: b, ctx: context.Background(), job: job}
		require.False(t, b.submitEasyApply(context.Background(), br, job, lazy, 8, "fixture", nil, llm.UsageSnapshot{}))
	}
	probe := navigateAndWait(t, br, job.URL)
	submits, err := probe.Eval(`()=>localStorage.getItem('submits')`)
	require.NoError(t, err)
	assert.Equal(t, "1", submits.Value.String())
	assert.Equal(t, int32(1), srv.requests.Load())
	var count int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM jobs_applied WHERE user_id=?`, b.cfg.UserID).Scan(&count))
	assert.Zero(t, count)
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM apply_attempts WHERE user_id=? AND outcome='submission_unknown'`, b.cfg.UserID).Scan(&count))
	assert.Equal(t, 2, count)
}

func TestWorkerBrowser_GreenhouseStaticSuccessCopyIsNotConfirmation(t *testing.T) {
	const destination = "https://boards.greenhouse.io/acmeinc/jobs/4567890"
	page := ghHijackPage(t, func(string) string {
		return `<html><body><form id="application_form"><label for="name">First name</label><input id="name"><button>Submit application</button></form><aside><h2>Application received</h2><p>Thank you for applying</p></aside></body></html>`
	})
	require.NoError(t, page.Navigate(destination))
	assert.False(t, greenhouseSubmissionConfirmed(page, "acmeinc", "4567890"))
	_, err := page.Eval(`()=>{document.querySelector('form').remove();document.querySelector('aside').remove();const confirmation=document.createElement('div');confirmation.id='application_confirmation';confirmation.textContent='Application received';document.body.appendChild(confirmation);}`)
	require.NoError(t, err)
	assert.True(t, greenhouseSubmissionConfirmed(page, "acmeinc", "4567890"))
	assert.False(t, greenhouseSubmissionConfirmed(page, "other", "4567890"))
	assert.False(t, greenhouseSubmissionConfirmed(page, "acmeinc", "another-job"))
}
