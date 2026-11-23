package bot

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llm"
	"testing"
)

func TestWorkerBrowser_NewControlAfterNextIsReinterpretedOnce(t *testing.T) {
	for _, changed := range []bool{true, false} {
		t.Run(fmt.Sprintf("changed=%t", changed), func(t *testing.T) {
			db := openMappingDB(t)
			settings := config.NewStore(db)
			require.NoError(t, settings.Set(domain.SystemUserID, keyApplyEngineConfig, domain.ApplyEngineConfig{Mode: domain.ApplyEngineAdaptive}))
			b := New(Config{DB: db, UserID: "conditional-fixture", Platform: domain.PlatformLinkedIn, Profile: basicProfile(), EngineStore: settings})
			html := fmt.Sprintf(`<html><body><main data-job-id="12345678"><h1>Coordinator at Acme Inc.</h1><div class="job-details-jobs-unified-top-card__company-name"><a href="https://www.linkedin.com/company/acme-inc">Acme Inc.</a></div><button id="cta">Easy Apply</button></main><script>document.getElementById('cta').onclick=()=>{const modal=document.createElement('div');modal.className='jobs-easy-apply-modal';modal.setAttribute('role','dialog');modal.setAttribute('aria-label','Easy Apply');modal.innerHTML='<h2>Apply to Acme Inc.</h2><progress value="1" max="2"></progress><div id="controls"><label for="name">First name</label><input id="name" required></div><button type="button" id="next" data-live-test-easy-apply-next-button>Next</button>';document.body.appendChild(modal);const next=modal.querySelector('button');next.onclick=()=>{localStorage.setItem('nextClicks',String(Number(localStorage.getItem('nextClicks')||0)+1));if(!document.getElementById('surname')){if(%t)document.getElementById('controls').insertAdjacentHTML('beforeend','<label for="surname">Surname</label><input id="surname" required>');return;}if(!document.getElementById('surname').value)return;if(!document.getElementById('email')){document.getElementById('controls').insertAdjacentHTML('beforeend','<label for="email">Email</label><input id="email" type="email" required>');return;}if(!document.getElementById('email').value)return;document.getElementById('controls').innerHTML='';modal.querySelector('progress').value=2;next.removeAttribute('data-live-test-easy-apply-next-button');next.setAttribute('data-live-test-easy-apply-submit-button','');next.textContent='Submit application';next.onclick=()=>{localStorage.setItem('submits','1');modal.textContent='Your application was sent to Acme Inc.';};};};</script></body></html>`, changed)
			browser := workerFixtureBrowser(t, func(string) string { return html })
			ctx := context.Background()
			job := linkedInJob{ID: "12345678", URL: "https://www.linkedin.com/jobs/view/12345678", Title: "Coordinator", Company: "Acme Inc."}
			success := b.submitEasyApply(ctx, browser, job, &lazyDocGen{b: b, ctx: ctx, job: job}, 8, "fixture", nil, llm.UsageSnapshot{})
			require.Equal(t, changed, success)
			page := navigateAndWait(t, browser, job.URL)
			result, err := page.Eval(`()=>[Number(localStorage.getItem('nextClicks')||0),Number(localStorage.getItem('submits')||0)]`)
			require.NoError(t, err)
			if changed {
				require.Equal(t, 3, result.Value.Get("0").Int())
				require.Equal(t, 1, result.Value.Get("1").Int())
			} else {
				require.Equal(t, 1, result.Value.Get("0").Int())
				require.Zero(t, result.Value.Get("1").Int())
			}
			var applied int
			require.NoError(t, db.QueryRow(`SELECT count(*) FROM jobs_applied WHERE user_id=?`, b.cfg.UserID).Scan(&applied))
			if changed {
				require.Equal(t, 1, applied)
			} else {
				require.Zero(t, applied)
			}
		})
	}
}

func TestAdaptiveRescan_UploadsCannotBeRepeated(t *testing.T) {
	require.True(t, interpretationUploads(StepInterpretation{Fields: []FieldResolution{{Action: ActionUpload}}}))
	require.False(t, interpretationUploads(StepInterpretation{Fields: []FieldResolution{{Action: ActionFill}}}))
}
