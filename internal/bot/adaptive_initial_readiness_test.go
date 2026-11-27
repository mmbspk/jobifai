package bot

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llm"
)

func linkedInReadinessHTML(id, initial string) string {
	return fmt.Sprintf(`<!doctype html><html><body><main data-job-id="%s"><h1>Coordinator</h1><div class="job-details-jobs-unified-top-card__company-name"><a href="https://www.linkedin.com/company/acme-inc">Acme Inc.</a></div><button id="cta">Easy Apply</button></main><script>
window.nextClicks=0;window.submits=0;
document.getElementById('cta').onclick=()=>{
 const modal=document.createElement('dialog');modal.id='application';
 modal.innerHTML=%q;if(!modal.querySelector('[data-sdui-screen]'))modal.setAttribute('role','dialog');document.body.appendChild(modal);modal.showModal();
 const next=modal.querySelector('#next');
 next.onclick=()=>{window.nextClicks++;localStorage.setItem('readinessNexts',String(window.nextClicks));const step=modal.querySelector('progress');
 if(step.value===1){step.value=2;modal.querySelector('#fields').innerHTML='<label for="name">First name</label><input id="name" required>';next.textContent='Review';return;}
 window.filledName=modal.querySelector('#name').value;localStorage.setItem('readinessName',window.filledName);step.value=3;modal.querySelector('#fields').innerHTML='<h3>Review your application</h3>';next.textContent='Submit application';
 next.onclick=()=>{window.submits++;localStorage.setItem('readinessSubmits',String(window.submits));modal.textContent='Your application was sent to Acme Inc.';};
 };
};</script></body></html>`, id, initial)
}

const linkedInReadinessIntro = `<div data-sdui-screen="com.linkedin.sdui.flagshipnav.jobs.easyapply.EasyApply"><h2>Apply to Acme Inc.</h2><progress value="1" max="3"></progress><div id="fields"><p>Before you begin, review the role's requirements.</p></div><button id="next" type="button">Continue</button></div>`

func TestAdaptiveInitialReadiness_EmptyIntroReachesCurrentFieldsAndConfirmedSubmission(t *testing.T) {
	for _, company := range []string{"Acme Inc.", "Acme Inc"} {
		t.Run(company, func(t *testing.T) {
			db := openMappingDB(t)
			settings := config.NewStore(db)
			require.NoError(t, settings.Set(domain.SystemUserID, keyApplyEngineConfig, domain.ApplyEngineConfig{Mode: domain.ApplyEngineAdaptive}))
			b := New(Config{DB: db, UserID: "initial-ready", Platform: domain.PlatformLinkedIn, Profile: basicProfile(), EngineStore: settings})
			br := workerFixtureBrowser(t, func(raw string) string { return linkedInReadinessHTML(linkedInExpectedID(raw), linkedInReadinessIntro) })
			job := linkedInJob{ID: "62345678", URL: "https://www.linkedin.com/jobs/view/62345678", Title: "Coordinator", Company: company}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			lazy := &lazyDocGen{b: b, ctx: ctx, job: job}
			require.True(t, b.submitEasyApply(ctx, br, job, lazy, 8, "fixture", nil, llm.UsageSnapshot{}))
			var count int
			require.NoError(t, db.QueryRow("SELECT count(*) FROM apply_attempts WHERE user_id=? AND job_id=? AND outcome='submitted_confirmed'", b.cfg.UserID, job.ID).Scan(&count))
			require.Equal(t, 1, count)
			probe := navigateAndWait(t, br, job.URL)
			value, err := probe.Eval(`()=>[localStorage.getItem('readinessNexts'),localStorage.getItem('readinessName'),localStorage.getItem('readinessSubmits')]`)
			require.NoError(t, err)
			require.Equal(t, "2", value.Value.Get("0").String())
			require.Equal(t, b.cfg.Profile.PersonalInformation.Name, value.Value.Get("1").String())
			require.Equal(t, "1", value.Value.Get("2").String())
		})
	}
}

func TestAdaptiveInitialReadiness_PreservesInitialProfileBindingHold(t *testing.T) {
	db := openMappingDB(t)
	profile := basicProfile()
	profile.EducationDetails = []domain.EducationDetail{{Institution: "South College", EducationLevel: "BSc", YearOfCompletion: "2010"}}
	b := New(Config{DB: db, UserID: "initial-hold", Platform: domain.PlatformLinkedIn, Profile: profile})
	initial := `<div data-sdui-screen="EasyApply"><h2>Apply to Acme Inc.</h2><fieldset><label for="school">Institution</label><input id="school" value="South College"><label for="degree">Degree</label><select id="degree"><option>BS</option><option>MS</option></select><label><input id="attend" type="checkbox" checked>I currently attend this institution</label></fieldset><button id="next">Next</button></div>`
	br := workerFixtureBrowser(t, func(raw string) string { return linkedInReadinessHTML(linkedInExpectedID(raw), initial) })
	page := navigateAndWait(t, br, "https://www.linkedin.com/jobs/view/62345678")
	job := linkedInJob{ID: "62345678", URL: "https://www.linkedin.com/jobs/view/62345678", Title: "Coordinator", Company: "Acme Inc."}
	ac := newAttemptContext(b.cfg.UserID, "linkedin", job.ID, job.URL, job.Title, job.Company, "readiness_fixture", "")
	ac.Engine = domain.ApplyEngineAdaptive
	beginAttempt(db, ac)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	err := b.adaptiveEasyApply(ctx, page, &lazyDocGen{b: b, ctx: ctx, job: job, attemptID: ac.AttemptID}, ac)
	require.ErrorIs(t, err, errAdaptiveQuestionReview)
	require.Contains(t, err.Error(), "scan initial step")
	require.Contains(t, err.Error(), "current-attendance")
	value, err := page.Eval(`()=>[window.nextClicks,window.submits]`)
	require.NoError(t, err)
	require.Zero(t, value.Value.Get("0").Int())
	require.Zero(t, value.Value.Get("1").Int())
	var stage string
	require.NoError(t, db.QueryRow("SELECT COALESCE(stage,'') FROM apply_attempts WHERE id=?", ac.AttemptID).Scan(&stage))
	require.NotEqual(t, "submit_dispatch", stage)
}

func TestAdaptiveInitialReadiness_UntrustedEmptyStepsDoNotNavigate(t *testing.T) {
	for _, tc := range []struct{ name, initial string }{
		{"loading", `<div data-sdui-screen="EasyApply" aria-busy="true"><h2>Apply to Acme Inc.</h2><button id="next">Continue</button></div>`},
		{"indeterminate spinner", `<div data-sdui-screen="EasyApply"><h2>Apply to Acme Inc.</h2><div role="progressbar" style="width:24px;height:24px"></div><button id="next">Continue</button></div>`},
		{"determinate loading", `<div data-sdui-screen="EasyApply"><h2>Apply to Acme Inc.</h2><div role="progressbar" aria-label="Loading form" aria-valuenow="50" style="width:24px;height:24px"></div><button id="next">Continue</button></div>`},
		{"loading marker", `<div data-sdui-screen="EasyApply"><h2>Apply to Acme Inc.</h2><div role="progressbar" data-testid="loading-form" aria-valuenow="50" style="width:24px;height:24px"></div><button id="next">Continue</button></div>`},
		{"unknown checkbox", `<div data-sdui-screen="EasyApply"><h2>Apply to Acme Inc.</h2><div role="checkbox" aria-checked="false" tabindex="0">Confirm eligibility</div><button id="next">Continue</button></div>`},
		{"unknown focusable widget", `<div data-sdui-screen="EasyApply"><h2>Apply to Acme Inc.</h2><div tabindex="0">Confirm eligibility</div><button id="next">Continue</button></div>`},
		{"unknown resume action", `<div data-sdui-screen="EasyApply"><h2>Apply to Acme Inc.</h2><button>Choose attachment</button><button id="next">Continue</button></div>`},
		{"wrong employer", `<div data-sdui-screen="EasyApply"><h2>Apply to Other Company</h2><button id="next">Continue</button></div>`},
		{"wrong subject", `<div data-sdui-screen="EasyApply" data-job-id="62345679"><h2>Apply to Acme Inc.</h2><button id="next">Continue</button></div>`},
		{"unrelated generic dialog", `<div><h2>Apply to Acme Inc.</h2><button id="next">Continue</button></div>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openMappingDB(t)
			b := New(Config{DB: db, UserID: "initial-negative", Platform: domain.PlatformLinkedIn, Profile: basicProfile()})
			br := workerFixtureBrowser(t, func(raw string) string { return linkedInReadinessHTML(linkedInExpectedID(raw), tc.initial) })
			page := navigateAndWait(t, br, "https://www.linkedin.com/jobs/view/62345678")
			job := linkedInJob{ID: "62345678", URL: "https://www.linkedin.com/jobs/view/62345678", Title: "Coordinator", Company: "Acme Inc."}
			ac := newAttemptContext(b.cfg.UserID, "linkedin", job.ID, job.URL, job.Title, job.Company, "readiness_fixture", "")
			ac.Engine = domain.ApplyEngineAdaptive
			beginAttempt(db, ac)
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			err := b.adaptiveEasyApply(ctx, page, &lazyDocGen{b: b, ctx: ctx, job: job, attemptID: ac.AttemptID}, ac)
			require.ErrorIs(t, err, context.DeadlineExceeded)
			value, err := page.Eval(`()=>[window.nextClicks,window.submits,!!document.getElementById('application')]`)
			require.NoError(t, err)
			require.Zero(t, value.Value.Get("0").Int())
			require.Zero(t, value.Value.Get("1").Int())
			require.True(t, value.Value.Get("2").Bool(), "fixture must reach the initial dialog before its readiness is rejected")
		})
	}
}
