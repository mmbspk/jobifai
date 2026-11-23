package bot

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llm"
)

func TestLinkedInConfirmation_NativeDialogKeepsJobAndRecipientOwnership(t *testing.T) {
	for _, tc := range []struct {
		name, owner, recipient string
		open, want             bool
	}{
		{"native dialog", "", "Acme Inc.", true, true},
		{"wrong owner", "52345679", "Acme Inc.", true, false},
		{"wrong recipient", "", "Other Company", true, false},
		{"closed dialog", "", "Acme Inc.", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			br := workerFixtureBrowser(t, func(raw string) string {
				owner, open := "", ""
				if tc.owner != "" {
					owner = `data-job-id="` + tc.owner + `"`
				}
				if tc.open {
					open = "open"
				}
				return fmt.Sprintf(`<html><body><main data-job-id="52345678"><h1>Coordinator at Acme Inc.</h1><div class="job-details-jobs-unified-top-card__company-name"><a href="https://www.linkedin.com/company/acme-inc">Acme Inc.</a></div><button>Easy Apply</button></main><dialog %s %s><p>Your application was sent to %s!</p><p>Track your application in the Applied tab of My Jobs</p></dialog></body></html>`, open, owner, tc.recipient)
			})
			page := br.MustPage("https://www.linkedin.com/jobs/view/52345678").MustWaitLoad()
			require.Equal(t, tc.want, linkedInSubmissionConfirmed(page, "52345678", "Acme Inc."))
		})
	}
}

func TestWorkerBrowser_LinkedInNativeConfirmationPersistsHistory(t *testing.T) {
	ctx := context.Background()
	db := openMappingDB(t)
	settings := config.NewStore(db)
	require.NoError(t, settings.Set(domain.SystemUserID, keyApplyEngineConfig, domain.ApplyEngineConfig{Mode: domain.ApplyEngineAdaptive}))
	srv := newGroundedCountingServer(t, func([]byte) string {
		return `[{"ref":"f1","action":"fill","intent_type":"profile","binding_key":"application_defaults.notice_period"}]`
	})
	b := New(Config{DB: db, UserID: "native-confirmation", Platform: domain.PlatformLinkedIn, Profile: basicProfile(), EngineStore: settings, AdaptiveMapping: &DBMappingStore{DB: db, UserID: "native-confirmation"}, AdaptiveLLM: NewLLMFormInterpreter(srv.newClient(t))})
	br := workerFixtureBrowser(t, func(raw string) string {
		html := linkedInWorkerHTML(linkedInExpectedID(raw), "Acme Inc.")
		return strings.Replace(html, `modal.textContent='Your application was sent to Acme Inc.';`, `modal.remove();const success=document.createElement('dialog');success.innerHTML='<p>Your application was sent to Acme Inc.!</p><p>Track your application in the Applied tab of My Jobs</p>';document.body.appendChild(success);success.showModal();`, 1)
	})
	job := linkedInJob{ID: "52345678", URL: "https://www.linkedin.com/jobs/view/52345678", Title: "Coordinator", Company: "Acme Inc."}
	lazy := &lazyDocGen{b: b, ctx: ctx, job: job}
	require.True(t, b.submitEasyApply(ctx, br, job, lazy, 8, "fixture", nil, llm.UsageSnapshot{}))
	var count int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM jobs_applied WHERE user_id=? AND id=?`, b.cfg.UserID, job.ID).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM apply_attempts WHERE user_id=? AND job_id=? AND outcome='submitted_confirmed' AND engine='adaptive'`, b.cfg.UserID, job.ID).Scan(&count))
	require.Equal(t, 1, count)
}

func TestLinkedInConfirmation_ApplicationStatusCardIsOwnedAndOutsideDescription(t *testing.T) {
	for _, tc := range []struct {
		name, attribute string
		want            bool
	}{
		{"owned status", "", true},
		{"quoted status in description", "data-job-description", false},
		{"other listing status", `data-job-id="52345679"`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			br := workerFixtureBrowser(t, func(string) string {
				return `<html><body><main data-job-id="52345678"><h1>Coordinator at Acme Inc.</h1><div class="job-details-jobs-unified-top-card__company-name"><a href="https://www.linkedin.com/company/acme-inc">Acme Inc.</a></div><button>Easy Apply</button><section ` + tc.attribute + `><div><h2>Application status</h2></div><div><p>Application submitted</p><p>now</p></div></section></main></body></html>`
			})
			page := br.MustPage("https://www.linkedin.com/jobs/view/52345678").MustWaitLoad()
			require.Equal(t, tc.want, linkedInSubmissionConfirmed(page, "52345678", "Acme Inc."))
		})
	}
}

func standaloneSubmittedStatusHTML() string {
	return `<html><body><main><div id=listing><div id=JobDetails_ManageJobBanner_52345678></div><div id=header><a aria-label="Company, Acme Inc." href="https://www.linkedin.com/company/acme-inc">Acme Inc.</a><p>Coordinator</p></div><section id=status><div><h2>Application status</h2></div><div><p>Application submitted</p><p>now</p></div></section><div id=JobDetails_AboutTheJob_52345678><p>Coordinate client projects.</p></div></div></main></body></html>`
}

func TestLinkedInConfirmation_StandaloneSubmittedStatusWithoutCTA(t *testing.T) {
	for _, tc := range []struct {
		name, mutation, title, company string
		want                           bool
	}{
		{"owned status with known metadata", "", "Coordinator", "Acme Inc.", true},
		{"missing declared title", "", "", "Acme Inc.", false},
		{"wrong declared title", "", "Other role", "Acme Inc.", false},
		{"wrong employer", "", "Coordinator", "Other Company", false},
		{"quoted in description", `()=>document.getElementById('JobDetails_AboutTheJob_52345678').appendChild(document.getElementById('status'))`, "Coordinator", "Acme Inc.", false},
		{"quoted in separate description widget", `()=>document.getElementById('status').setAttribute('data-job-description','')`, "Coordinator", "Acme Inc.", false},
		{"another listing", `()=>document.getElementById('status').setAttribute('data-job-id','52345679')`, "Coordinator", "Acme Inc.", false},
		{"two status cards", `()=>document.getElementById('status').after(document.getElementById('status').cloneNode(true))`, "Coordinator", "Acme Inc.", false},
		{"unrelated recommendation", `()=>{const aside=document.createElement('aside');document.body.appendChild(aside);aside.appendChild(document.getElementById('status'));}`, "Coordinator", "Acme Inc.", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			br := workerFixtureBrowser(t, func(string) string { return standaloneSubmittedStatusHTML() })
			page := br.MustPage("https://www.linkedin.com/jobs/view/52345678").MustWaitLoad()
			if tc.mutation != "" {
				_, err := page.Eval(tc.mutation)
				require.NoError(t, err)
			}
			require.Equal(t, tc.want, linkedInSubmissionConfirmed(page, "52345678", tc.company, tc.title))
			if tc.want {
				require.True(t, linkedInSubmitReconciled(page, "https://www.linkedin.com/jobs/view/52345678", tc.company, tc.title))
			}
		})
	}
}
