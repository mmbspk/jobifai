package bot

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llm"
)

const seekSentBannerFixture = `<div><h1 id="applicationSent">Nice work</h1><span>Your application has been sent to Acme Inc.</span></div>`

func TestSeekNativeConfirmation_ExactRouteEmployerAndOwnedBanner(t *testing.T) {
	const original = "https://au.seek.com/job/12345678"
	for _, tc := range []struct {
		name, route, body, company string
		want                       bool
	}{
		{"current sent banner", original + "/apply/success", seekSentBannerFixture, "Acme Inc.", true},
		{"wrong job", "https://au.seek.com/job/22345678/apply/success", seekSentBannerFixture, "Acme Inc.", false},
		{"review route", original + "/apply/review", seekSentBannerFixture, "Acme Inc.", false},
		{"untrusted destination", "https://example.com/job/12345678/apply/success", seekSentBannerFixture, "Acme Inc.", false},
		{"wrong employer", original + "/apply/success", seekSentBannerFixture, "Other Company", false},
		{"unknown employer", original + "/apply/success", seekSentBannerFixture, "", false},
		{"hidden banner", original + "/apply/success", `<div hidden>` + seekSentBannerFixture + `</div>`, "Acme Inc.", false},
		{"duplicate current banner", original + "/apply/success", seekSentBannerFixture + seekSentBannerFixture, "Acme Inc.", false},
		{"recommendation decoy", original + "/apply/success", `<div data-testid="similar-jobs-carousel">` + seekSentBannerFixture + `</div>`, "Acme Inc.", false},
		{"unowned body text", original + "/apply/success", `<h1>Recommended jobs</h1><p>Your application has been sent to Acme Inc.</p>`, "Acme Inc.", false},
		{"wrong neighbouring employer", original + "/apply/success", `<div><h1 id="applicationSent">Nice work</h1><span>Your application has been sent to Other Company</span></div><aside><span>Your application has been sent to Acme Inc.</span></aside>`, "Acme Inc.", false},
		{"ambiguous message", original + "/apply/success", `<div><h1 id="applicationSent">Nice work</h1><span>Your application has been sent to Acme Inc.</span><span>Your application has been sent to Other Company</span></div>`, "Acme Inc.", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			br := workerFixtureBrowser(t, func(string) string { return "<html><body>" + tc.body + "</body></html>" })
			page := navigateAndWait(t, br, tc.route)
			require.Equal(t, tc.want, seekNativeSubmissionConfirmed(page, "12345678", original, tc.company))
		})
	}
}

func TestSeekNativeConfirmation_WorkerPersistsHistoryWithoutJobPageBadge(t *testing.T) {
	db := openMappingDB(t)
	settings := config.NewStore(db)
	require.NoError(t, settings.Set(domain.SystemUserID, keyApplyEngineConfig, domain.ApplyEngineConfig{Mode: domain.ApplyEngineAdaptive}))
	b := New(Config{DB: db, UserID: "seek-sent-fixture", Platform: domain.PlatformSeek, Profile: basicProfile(), EngineStore: settings})
	br := workerFixtureBrowser(t, func(raw string) string {
		if !strings.Contains(raw, "/apply") {
			return `<html><body><h1>Coordinator</h1><a data-automation="job-detail-company-name">Acme Inc.</a><a data-automation="job-detail-apply" href="/job/12345678/apply">Quick Apply</a></body></html>`
		}
		html := strings.ReplaceAll(seekWorkerHTML(raw), "How soon can you leave?", "Notice period")
		return strings.Replace(html, `form.innerHTML='<h1 data-automation="application-success-title">Application submitted</h1>'`, `history.pushState({},'', '/job/12345678/apply/success');form.innerHTML='`+seekSentBannerFixture+`'`, 1)
	})
	job := seekJob{ID: "12345678", URL: "https://au.seek.com/job/12345678", Title: "Coordinator", Company: "Acme Inc.", EasyApply: true}
	lazy := &lazyDocGen{b: b, ctx: context.Background(), job: linkedInJob{ID: job.ID, URL: job.URL, Title: job.Title, Company: job.Company}}
	require.Equal(t, seekSubmitApplied, b.submitSeekApplication(context.Background(), br, job, lazy, 8, "fixture", nil, llm.UsageSnapshot{}))
	var count int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM jobs_applied WHERE user_id=? AND id=?`, b.cfg.UserID, job.ID).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM apply_attempts WHERE user_id=? AND job_id=? AND outcome='submitted_confirmed'`, b.cfg.UserID, job.ID).Scan(&count))
	require.Equal(t, 1, count)
}
