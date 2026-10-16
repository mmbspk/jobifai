package bot

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/require"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/llm"
	"os"
	"path/filepath"
	"testing"
)

func TestLinkedInResolver_CapturedSDUIPages(t *testing.T) {
	for _, tc := range []struct{ name, id, kind, description string }{
		{"external", "4479800927", "external", "Project Delivery:"},
		{"native", "4477930880", "native", "Australian citizenship"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			br := launchTestBrowser(t)
			html, err := os.ReadFile(filepath.Join("testdata", "linkedin", "sdui-"+tc.name+".html"))
			require.NoError(t, err)
			srv := fixtureServer(t, tc.id, "", string(html), "")
			page := navigateAndWait(t, br, srv.URL+"/jobs/view/"+tc.id)
			r, err := resolveLinkedInJob(page, tc.id, false)
			require.NoError(t, err)
			require.True(t, r.IdentityVerified)
			require.Equal(t, tc.kind, r.Kind)
			require.Contains(t, r.Description, tc.description)
			require.Greater(t, len(r.Description), 1000)
			require.NotContains(t, r.Description, "More jobs")
			require.NoError(t, verifyJobIdentity(page, "/jobs/view/"+tc.id))
		})
	}
}

func TestSubmitEasyApply_CapturedSDUINative(t *testing.T) {
	br := launchTestBrowser(t)
	html, err := os.ReadFile(filepath.Join("testdata", "linkedin", "sdui-native.html"))
	require.NoError(t, err)
	// Keep the captured listing header/description intact. Only the modal and
	// its confirmation are synthetic; this never connects to LinkedIn.
	html = append(html, []byte(`<script>
document.querySelector('[aria-label="Easy Apply to this job"]').onclick=function(){
 this.remove();
 const modal=document.createElement('div');
 modal.className='jobs-easy-apply-modal';modal.setAttribute('role','dialog');modal.setAttribute('aria-label','Easy Apply');
 const submit=document.createElement('button');submit.setAttribute('data-live-test-easy-apply-submit-button','');submit.textContent='Submit application';
 submit.onclick=()=>{modal.textContent='Your application was sent to Exceed Global';};
 modal.appendChild(submit);document.body.appendChild(modal);
};
</script>`)...)
	srv := fixtureServer(t, "4477930880", "", string(html), "")
	db := testDB(t)
	b := &Bot{cfg: Config{DB: db, UserID: "u1"}, stopCh: make(chan struct{})}
	job := linkedInJob{ID: "4477930880", URL: srv.URL + "/jobs/view/4477930880", Title: "Principal Software Engineer (EL2)", Company: "Exceed Global"}
	lazy := &lazyDocGen{b: b, job: job, ctx: context.Background()}
	require.True(t, b.submitEasyApply(context.Background(), br, job, lazy, 8, "Evaluated intended listing", nil, llm.UsageSnapshot{}))
	var outcome string
	require.NoError(t, db.QueryRow(`SELECT outcome FROM apply_attempts WHERE user_id='u1' AND job_id=?`, job.ID).Scan(&outcome))
	require.Equal(t, string(OutcomeSubmittedConfirmed), outcome)
}

func TestLinkedInResolver_DetectAndClickAgree(t *testing.T) {
	br := launchTestBrowser(t)
	html := `<main data-job-id="4479748087"><h1>MYOB</h1><a href="https://employer.example/apply">Apply</a><section data-job-id="4478417422"><button onclick="window.wrongClicks=(window.wrongClicks||0)+1">Easy Apply</button></section></main>`
	srv := fixtureServer(t, "4479748087", "", html, "")
	page := navigateAndWait(t, br, srv.URL+"/jobs/view/4479748087")
	for _, click := range []bool{false, true} {
		r, err := resolveLinkedInJob(page, "4479748087", click)
		require.NoError(t, err)
		require.True(t, r.IdentityVerified)
		require.Equal(t, "external", r.Kind)
		require.Empty(t, r.Action)
	}
	value, err := page.Eval(`() => window.wrongClicks || 0`)
	require.NoError(t, err)
	require.Equal(t, 0, value.Value.Int())
}

func TestLinkedInResolver_NativeSingleClickAndDrift(t *testing.T) {
	br := launchTestBrowser(t)
	html := `<main data-job-id="4479748087"><button onclick="window.clicks=(window.clicks||0)+1">Easy Apply</button></main>`
	srv := fixtureServer(t, "4479748087", "", html, "")
	page := navigateAndWait(t, br, srv.URL+"/jobs/view/4479748087")
	r, err := resolveLinkedInJob(page, "4479748087", false)
	require.NoError(t, err)
	require.Equal(t, "native", r.Kind)
	r, err = resolveLinkedInJob(page, "4479748087", true)
	require.NoError(t, err)
	require.Equal(t, "clicked", r.Action)
	v, err := page.Eval(`() => window.clicks`)
	require.NoError(t, err)
	require.Equal(t, 1, v.Value.Int())
	_, err = page.Eval(`() => history.replaceState({},'', '/jobs/collections/similar-jobs/?currentJobId=4478417422&referenceJobId=4479748087')`)
	require.NoError(t, err)
	r, err = resolveLinkedInJob(page, "4479748087", true)
	require.NoError(t, err)
	require.False(t, r.IdentityVerified)
	require.Empty(t, r.Action)
	v, err = page.Eval(`() => window.clicks`)
	require.NoError(t, err)
	require.Equal(t, 1, v.Value.Int())
}

func TestLinkedInReview_PreservesDocumentsAndUserScope(t *testing.T) {
	db, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	defer db.Close()
	b := &Bot{cfg: Config{DB: db, UserID: "u1"}, seenCache: newJobSeenCache()}
	job := linkedInJob{ID: "4479748087", URL: "https://www.linkedin.com/jobs/view/4479748087", Title: "Role", Company: "Company"}
	_, err = db.Exec(`INSERT INTO jobs_pending_review(job_id,user_id,company,role,platform,link,document_refs_json,created_at) VALUES(?, 'u1','Company','Role','linkedin',?,'{"resume":"prepared"}',datetime('now'))`, job.ID, job.URL)
	require.NoError(t, err)
	b.preserveLinkedInReview(context.Background(), job, 8, "match", errLinkedInIdentity)
	_, err = db.Exec(`UPDATE jobs_pending_review SET resume_path='existing.pdf', attempt_count=1 WHERE job_id=?`, job.ID)
	require.NoError(t, err)
	b.preserveLinkedInReview(context.Background(), job, 8, "match", errLinkedInSubmissionUnknown)
	var path, pack string
	var attempts int
	require.NoError(t, db.QueryRow(`SELECT resume_path,attempt_count,document_refs_json FROM jobs_pending_review WHERE job_id=? AND user_id='u1'`, job.ID).Scan(&path, &attempts, &pack))
	require.Equal(t, "existing.pdf", path)
	require.Equal(t, 1, attempts)
	require.Contains(t, pack, "prepared")
	var reason string
	require.NoError(t, db.QueryRow(`SELECT skip_reason FROM jobs_skipped WHERE id=? AND user_id='u1'`, job.ID).Scan(&reason))
	require.Contains(t, reason, "submission_unknown")
	other := &Bot{cfg: Config{DB: db, UserID: "u2"}}
	other.preserveLinkedInReview(context.Background(), job, 8, "", errLinkedInIdentity)
	require.NoError(t, db.QueryRow(`SELECT document_refs_json FROM jobs_pending_review WHERE job_id=? AND user_id='u1'`, job.ID).Scan(&pack))
	require.Contains(t, pack, "prepared")
}

func TestLinkedInReviewOutcome(t *testing.T) {
	for _, tc := range []struct {
		err     error
		outcome AttemptOutcome
		code    string
	}{
		{errLinkedInIdentity, OutcomeBlocked, "linkedin_listing_unverified"},
		{errLinkedInUnknown, OutcomeBlocked, "linkedin_listing_unverified"},
		{errLinkedInExternal, OutcomeBlocked, "external_apply"},
		{errLinkedInSubmissionUnknown, OutcomeSubmissionUnknown, "linkedin_submit_unconfirmed"},
	} {
		err := fmt.Errorf("wrapped: %w", tc.err)
		require.True(t, linkedInNeedsReview(err))
		outcome, _, code := queueApplyOutcome(err)
		require.Equal(t, tc.outcome, outcome)
		require.Equal(t, tc.code, code)
	}
}

func TestLinkedInReview_ScoredHoldRetainsHalalVerdict(t *testing.T) {
	db := testDB(t)
	b := &Bot{cfg: Config{DB: db, UserID: "u1"}, seenCache: newJobSeenCache()}
	job := linkedInJob{ID: "123", URL: "https://www.linkedin.com/jobs/view/123", Title: "Role", Company: "Company"}
	b.preserveLinkedInReviewWithVerdict(context.Background(), job, 8, "Evaluated intended listing", []byte(`{"verdict":"DOUBTFUL"}`), errLinkedInUnknown)
	var score int
	var reasoning, verdict string
	require.NoError(t, db.QueryRow(`SELECT suitability_score,suitability_reasoning,halal_verdict FROM jobs_skipped WHERE id='123' AND user_id='u1'`).Scan(&score, &reasoning, &verdict))
	require.Equal(t, 8, score)
	require.Equal(t, "Evaluated intended listing", reasoning)
	require.Contains(t, verdict, "DOUBTFUL")
}

func TestLinkedInPrepare_DriftStopsBeforeFormScan(t *testing.T) {
	br := launchTestBrowser(t)
	srv := fixtureServer(t, "4478417422", "", `<main data-job-id="4478417422"><button>Next</button></main>`, "")
	page := navigateAndWait(t, br, srv.URL+"/jobs/view/4478417422")
	b := &Bot{}
	lazy := &lazyDocGen{b: b, job: linkedInJob{ID: "4479748087", URL: srv.URL + "/jobs/view/4479748087"}, reviewPrepareOnly: true}
	require.ErrorIs(t, b.scanApplyStepsForPrepare(context.Background(), page, lazy), errLinkedInIdentity)
	require.False(t, lazy.prepareScanComplete)
}

func TestSubmitEasyApply_VerifiedNativeConfirmation(t *testing.T) {
	br := launchTestBrowser(t)
	html := `<main data-job-id="4479748087"><h1>Verified role</h1>
 <button onclick="this.remove();document.getElementById('modal').style.display='block'">Easy Apply</button>
 <div id="modal" class="jobs-easy-apply-modal" role="dialog" aria-label="Easy Apply" style="display:none">
 <button data-live-test-easy-apply-submit-button onclick="this.parentElement.innerHTML='Your application was sent to Company'">Submit application</button>
 </div></main>`
	srv := fixtureServer(t, "4479748087", "", html, "")
	db, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	defer db.Close()
	b := &Bot{cfg: Config{DB: db, UserID: "u1"}, stopCh: make(chan struct{})}
	job := linkedInJob{ID: "4479748087", URL: srv.URL + "/jobs/view/4479748087", Title: "Role", Company: "Company"}
	lazy := &lazyDocGen{b: b, job: job, ctx: context.Background()}
	require.True(t, b.submitEasyApply(context.Background(), br, job, lazy, 8, "match", nil, llm.UsageSnapshot{}))
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM jobs_applied WHERE user_id='u1' AND link=?`, job.URL).Scan(&count))
	require.Equal(t, 1, count)
	var outcome string
	require.NoError(t, db.QueryRow(`SELECT outcome FROM apply_attempts WHERE user_id='u1' AND job_id=?`, job.ID).Scan(&outcome))
	require.Equal(t, string(OutcomeSubmittedConfirmed), outcome)
}

func TestLinkedInConfirmation_RequiresVerifiedJobAndEmployer(t *testing.T) {
	br := launchTestBrowser(t)
	html := `<main data-job-id="4479748087"><h1>Role</h1><button>Easy Apply</button></main><div role="dialog">Your application was sent to OtherCo</div>`
	srv := fixtureServer(t, "4479748087", "", html, "")
	page := navigateAndWait(t, br, srv.URL+"/jobs/view/4479748087")
	require.False(t, linkedInSubmissionConfirmed(page, "4479748087", "Company"))
	_, err := page.Eval(`() => document.querySelector('[role="dialog"]').textContent='Your application was sent to Company'`)
	require.NoError(t, err)
	require.True(t, linkedInSubmissionConfirmed(page, "4479748087", "Company"))
	require.False(t, linkedInSubmissionConfirmed(page, "4478417422", "Company"))
}
