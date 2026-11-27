package bot

import (
	"context"
	"fmt"
	"html"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llm"
)

// The fixture records native form input and navigation in origin-local storage,
// so the caller-owned page can close without hiding an unexpected application.
func workPreferenceWorkerHTML(id, question string) string {
	page := linkedInWorkerHTML(id, "Acme Inc.")
	radio := fmt.Sprintf(`<fieldset><legend>%s</legend><label><input id=hybrid-yes name=hybrid type=radio value=yes required>Yes</label><label><input id=hybrid-no name=hybrid type=radio value=no required>No</label></fieldset>`, html.EscapeString(question))
	page = strings.Replace(page, `<label for="notice">How soon can you leave?</label><input id="notice">`, radio, 1)
	page = strings.Replace(page, `localStorage.setItem('lastNotice',document.getElementById('notice').value);`, `const selected=document.querySelector('input[name=hybrid]:checked');if(!selected)return;localStorage.setItem('hybrid-choice',selected.value);`, 1)
	return page + `<script>
 const openPreferenceForm=document.getElementById('cta').onclick;
 document.getElementById('cta').onclick=event=>{
  openPreferenceForm(event);localStorage.setItem('preference-modal-open','true');
  document.querySelectorAll('input[name=hybrid]').forEach(input=>input.addEventListener('change',()=>localStorage.setItem('preference-input-touched','true')));
 };
 </script>`
}

func TestWorkerBrowser_HybridPreferenceCompletesAndLearnsWithoutModel(t *testing.T) {
	database := openMappingDB(t)
	settings := config.NewStore(database)
	require.NoError(t, settings.Set(domain.SystemUserID, keyApplyEngineConfig, domain.ApplyEngineConfig{Mode: domain.ApplyEngineAdaptive}))
	profile := basicProfile()
	profile.ApplicationDefaults.FormAnswers = nil
	recorder := &answerRecorderStub{}
	store := &DBMappingStore{DB: database, UserID: "preference-browser"}
	b := New(Config{DB: database, UserID: store.UserID, Platform: domain.PlatformLinkedIn, Profile: profile, Preferences: domain.WorkPreferences{Hybrid: true}, EngineStore: settings, AdaptiveMapping: store, ApplicationAnswers: recorder})
	job := linkedInJob{ID: "82345678", URL: "https://www.linkedin.com/jobs/view/82345678", Title: "Coordinator", Company: "Acme Inc."}
	browser := workerFixtureBrowser(t, func(raw string) string {
		return workPreferenceWorkerHTML(linkedInExpectedID(raw), "Are you comfortable working in a hybrid setting?*")
	})
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
	defer cancel()
	lazy := &lazyDocGen{b: b, ctx: ctx, job: job}
	require.True(t, b.submitEasyApply(ctx, browser, job, lazy, 8, "fixture", nil, llm.UsageSnapshot{}), "a selected hybrid preference is a current candidate fact")
	require.Empty(t, recorder.proposals, "this known preference must not create a missing-answer review")
	var applied, confirmed int
	require.NoError(t, database.QueryRow("SELECT COUNT(*) FROM jobs_applied WHERE user_id=? AND id=?", b.cfg.UserID, job.ID).Scan(&applied))
	require.Equal(t, 1, applied)
	require.NoError(t, database.QueryRow("SELECT COUNT(*) FROM apply_attempts WHERE user_id=? AND job_id=? AND outcome='submitted_confirmed'", b.cfg.UserID, job.ID).Scan(&confirmed))
	require.Equal(t, 1, confirmed)
	var source, intent, provenance, value string
	require.NoError(t, database.QueryRow("SELECT source,intent_type,provenance,resolved_value FROM form_field_mappings WHERE user_id=? AND binding_key='work_preferences.hybrid'", b.cfg.UserID).Scan(&source, &intent, &provenance, &value))
	require.Equal(t, "preferences", source)
	require.Equal(t, "preferences", intent)
	require.Equal(t, string(ProvenanceTrustedReference), provenance)
	require.Empty(t, value, "reusable preference bindings must not retain an applicant answer")
	probe := navigateAndWait(t, browser, job.URL)
	state, err := probe.Eval(`()=>({opened:localStorage.getItem('preference-modal-open'),choice:localStorage.getItem('hybrid-choice'),touched:localStorage.getItem('preference-input-touched'),submits:localStorage.getItem('submits')})`)
	require.NoError(t, err)
	require.Equal(t, "true", state.Value.Get("opened").String())
	require.Equal(t, "yes", state.Value.Get("choice").String())
	require.Equal(t, "true", state.Value.Get("touched").String())
	require.Equal(t, "1", state.Value.Get("submits").String())
}

func TestWorkerBrowser_HybridPreferenceDoesNotGrantUnsupportedCommitment(t *testing.T) {
	for _, tc := range []struct {
		name, question string
		hybrid         bool
	}{
		{"unselected preference", "Are you comfortable working in a hybrid setting?", false},
		{"specific commute", "Are you comfortable working in a hybrid setting in Sydney three days each week?", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database := openMappingDB(t)
			settings := config.NewStore(database)
			require.NoError(t, settings.Set(domain.SystemUserID, keyApplyEngineConfig, domain.ApplyEngineConfig{Mode: domain.ApplyEngineAdaptive}))
			profile := basicProfile()
			profile.ApplicationDefaults.FormAnswers = nil
			b := New(Config{DB: database, UserID: "preference-negative", Platform: domain.PlatformLinkedIn, Profile: profile, Preferences: domain.WorkPreferences{Hybrid: tc.hybrid}, EngineStore: settings})
			job := linkedInJob{ID: "82345679", URL: "https://www.linkedin.com/jobs/view/82345679", Title: "Coordinator", Company: "Acme Inc."}
			browser := workerFixtureBrowser(t, func(raw string) string { return workPreferenceWorkerHTML(linkedInExpectedID(raw), tc.question) })
			ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
			defer cancel()
			lazy := &lazyDocGen{b: b, ctx: ctx, job: job}
			require.False(t, b.submitEasyApply(ctx, browser, job, lazy, 8, "fixture", nil, llm.UsageSnapshot{}))
			var applied, irreversible int
			require.NoError(t, database.QueryRow("SELECT COUNT(*) FROM jobs_applied WHERE user_id=?", b.cfg.UserID).Scan(&applied))
			require.Zero(t, applied)
			require.NoError(t, database.QueryRow("SELECT COUNT(*) FROM apply_attempts WHERE user_id=? AND (stage='submit_dispatch' OR outcome IN ('submitted_confirmed','submission_unknown'))", b.cfg.UserID).Scan(&irreversible))
			require.Zero(t, irreversible)
			probe := navigateAndWait(t, browser, job.URL)
			state, err := probe.Eval(`()=>({opened:localStorage.getItem('preference-modal-open'),choice:localStorage.getItem('hybrid-choice'),touched:localStorage.getItem('preference-input-touched'),submits:localStorage.getItem('submits')})`)
			require.NoError(t, err)
			require.Equal(t, "true", state.Value.Get("opened").String(), "the fixture must reach its form before the unsupported commitment is rejected")
			require.True(t, state.Value.Get("choice").Nil())
			require.True(t, state.Value.Get("touched").Nil())
			require.True(t, state.Value.Get("submits").Nil())
		})
	}
}

func TestWorkerBrowser_HybridPreferenceSeekCompletesWithoutModel(t *testing.T) {
	database := openMappingDB(t)
	settings := config.NewStore(database)
	require.NoError(t, settings.Set(domain.SystemUserID, keyApplyEngineConfig, domain.ApplyEngineConfig{Mode: domain.ApplyEngineAdaptive}))
	profile := basicProfile()
	profile.ApplicationDefaults.FormAnswers = nil
	recorder := &answerRecorderStub{}
	store := &DBMappingStore{DB: database, UserID: "seek-preference-browser"}
	b := New(Config{DB: database, UserID: store.UserID, Platform: domain.PlatformSeek, Profile: profile, Preferences: domain.WorkPreferences{Hybrid: true}, EngineStore: settings, AdaptiveMapping: store, ApplicationAnswers: recorder})
	b.cfg.Settings.HumanBehavior.PauseBetweenJobsMin = 1
	b.cfg.Settings.HumanBehavior.PauseBetweenJobsMax = 2
	browser := workerFixtureBrowser(t, func(raw string) string {
		page := seekWorkerHTML(raw)
		if !strings.Contains(raw, "/apply") {
			return page
		}
		page = strings.Replace(page, `<label for="notice">How soon can you leave?</label><input id="notice">`, `<fieldset><legend>Are you willing to work in a hybrid setting?</legend><label><input name=hybrid type=radio value=yes required>Yes</label><label><input name=hybrid type=radio value=no required>No</label></fieldset>`, 1)
		return strings.Replace(page, `localStorage.setItem('lastNotice',document.getElementById('notice').value);`, `const selected=document.querySelector('input[name=hybrid]:checked');if(!selected)return;localStorage.setItem('hybrid-choice',selected.value);`, 1)
	})
	job := seekJob{ID: "82345680", URL: "https://au.seek.com/job/82345680", Title: "Coordinator", Company: "Acme Inc.", EasyApply: true}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	lazy := &lazyDocGen{b: b, ctx: ctx, job: linkedInJob{ID: job.ID, URL: job.URL, Title: job.Title, Company: job.Company}}
	require.Equal(t, seekSubmitApplied, b.submitSeekApplication(ctx, browser, job, lazy, 8, "fixture", nil, llm.UsageSnapshot{}))
	require.Empty(t, recorder.proposals)
	var confirmed int
	require.NoError(t, database.QueryRow("SELECT COUNT(*) FROM apply_attempts WHERE user_id=? AND job_id=? AND outcome='submitted_confirmed'", b.cfg.UserID, job.ID).Scan(&confirmed))
	require.Equal(t, 1, confirmed)
	var source, intent, value string
	require.NoError(t, database.QueryRow("SELECT source,intent_type,resolved_value FROM form_field_mappings WHERE user_id=? AND binding_key='work_preferences.hybrid'", b.cfg.UserID).Scan(&source, &intent, &value))
	require.Equal(t, "preferences", source)
	require.Equal(t, "preferences", intent)
	require.Empty(t, value)
	probe := navigateAndWait(t, browser, job.URL)
	state, err := probe.Eval(`()=>({choice:localStorage.getItem('hybrid-choice'),submits:localStorage.getItem('submits')})`)
	require.NoError(t, err)
	require.Equal(t, "yes", state.Value.Get("choice").String())
	require.Equal(t, "1", state.Value.Get("submits").String())
}
