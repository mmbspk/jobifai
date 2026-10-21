package bot

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/require"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/llm"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func refreshedControlFixture(mutation string) string {
	return `<main data-job-id="123"><h1>Role at Company</h1><button id="cta">Easy Apply</button><div data-job-description>` + strings.Repeat("Verified job responsibilities and requirements. ", 8) + `<button onclick="` + mutation + `">Show more</button></div></main>`
}

func TestLinkedInURLListing_UsesRefreshedControl(t *testing.T) {
	br := launchTestBrowser(t)
	for _, tc := range []struct{ name, mutation, kind, outcome string }{
		{"native", "void 0", "native", string(OutcomeInProgress)},
		{"temporarily hidden", "document.getElementById('cta').style.display='none';setTimeout(()=>document.getElementById('cta').style.display='block',700)", "native", string(OutcomeInProgress)},
		{"external", "document.getElementById('cta').outerHTML='<a href=&quot;https://employer.example/apply&quot;>Apply</a>'", "external", string(OutcomeInProgress)},
		{"unknown", "document.getElementById('cta').remove()", "unknown", string(OutcomeBlocked)},
		{"already applied", "const c=document.getElementById('cta');c.textContent='Applied';c.setAttribute('data-test-job-apply-button-applied','')", "already_applied", string(OutcomeAlreadyApplied)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := fixtureServer(t, "123", "", refreshedControlFixture(tc.mutation), "")
			page := navigateAndWait(t, br, srv.URL+"/jobs/view/123")
			db := testDB(t)
			b := New(Config{DB: db, UserID: "u1"})
			job := linkedInJob{ID: "123", URL: srv.URL + "/jobs/view/123", Title: "Role", Company: "Company"}
			initial, err := waitLinkedInJob(context.Background(), page, "123", job)
			require.NoError(t, err)
			require.Equal(t, "native", initial.Kind)
			ac := newAttemptContext("u1", "linkedin", job.ID, job.URL, job.Title, job.Company, "applyFromURL")
			beginAttempt(db, ac)
			resolved, err := b.readLinkedInURLListing(context.Background(), page, job, ac)
			require.Equal(t, tc.kind, resolved.Kind)
			switch tc.kind {
			case "unknown":
				require.ErrorIs(t, err, errLinkedInUnknown)
			case "already_applied":
				require.ErrorIs(t, err, ErrAlreadyApplied)
			default:
				require.NoError(t, err)
			}
			var outcome string
			require.NoError(t, db.QueryRow(`SELECT outcome FROM apply_attempts WHERE id=?`, ac.AttemptID).Scan(&outcome))
			require.Equal(t, tc.outcome, outcome)
			var count int
			require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM jobs_pending_review`).Scan(&count))
			require.Zero(t, count)
			require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM jobs_applied`).Scan(&count))
			require.Zero(t, count)
			if tc.kind == "unknown" {
				var code string
				require.NoError(t, db.QueryRow(`SELECT failure_code FROM jobs_skipped WHERE id='123' AND user_id='u1'`).Scan(&code))
				require.Equal(t, "control_unrecognized", code)
			}
		})
	}
}

func TestProcessJob_AlreadyAppliedAfterDescriptionExpansion(t *testing.T) {
	br := launchTestBrowser(t)
	srv := fixtureServer(t, "123", "", refreshedControlFixture("const c=document.getElementById('cta');c.textContent='Applied';c.setAttribute('data-test-job-apply-button-applied','')"), "")
	db := testDB(t)
	scorer := &capturingScorer{}
	b := New(Config{DB: db, UserID: "u1", Scorer: scorer})
	job := linkedInJob{ID: "123", URL: srv.URL + "/jobs/view/123", Title: "Role", Company: "Company"}
	require.False(t, b.processJob(context.Background(), br, job))
	require.Empty(t, scorer.gotDesc)
	var reason string
	require.NoError(t, db.QueryRow(`SELECT skip_reason FROM jobs_skipped WHERE id='123' AND user_id='u1'`).Scan(&reason))
	require.Equal(t, "already_applied_indicator", reason)
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM jobs_pending_review`).Scan(&count))
	require.Zero(t, count)
}

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

// TestReadLinkedInDescription_SDUIDescriptionRemovedAndReinsertedOnExpand is a
// regression for the React re-render race described in PR #105.
//
// The fixture starts with an unusable short description so the reader cannot
// fall back to the pre-click snapshot. Clicking the expand button hides the
// element for 500 ms, then restores it with the full description. The reader
// must wait through the removal window and recover the expanded content.
//
// This test will fail if the retry loop exits prematurely on identity recovery
// without waiting for usable description content, or if the initial (short) read
// is silently returned.
func TestReadLinkedInDescription_SDUIDescriptionRemovedAndReinsertedOnExpand(t *testing.T) {
	br := launchTestBrowser(t)
	jobID := "55512345"
	// delayMs=500: element hidden for 500 ms; first retry (~400 ms) catches it
	// absent; second retry (~600 ms) sees the restored element with full text.
	srv := fixtureServer(t, jobID, "", sduiShortThenReRenderFixture(jobID, 500), "")
	page := navigateAndWait(t, br, srv.URL+"/jobs/view/"+jobID)
	job := linkedInJob{
		ID:      jobID,
		URL:     srv.URL + "/jobs/view/" + jobID,
		Title:   "Test Role",
		Company: "TestCo",
	}
	r, err := readLinkedInDescription(context.Background(), page, job)
	require.NoError(t, err)
	require.True(t, r.IdentityVerified)
	require.Contains(t, r.Description, "relevant field")
	require.Greater(t, len(r.Description), 100)
	simulated, evalErr := page.Eval(`() => window._rerenderSimulated === true`)
	require.NoError(t, evalErr)
	require.True(t, simulated.Value.Bool(), "expandable-text-button must have been clicked to trigger re-render simulation")
}

// sduiShortThenReRenderFixture returns SDUI HTML where the description element
// starts with a short, unusable placeholder (< 100 chars). Clicking the expand
// button hides the element for delayMs, then restores it with the full description.
// This simulates the React re-render race: the reader must wait through the removal
// window rather than returning the pre-click content.
func sduiShortThenReRenderFixture(jobID string, delayMs int) string {
	fullBody := strings.Repeat("The position requires demonstrated experience and strong skills in the relevant field. ", 5)
	scriptPart := fmt.Sprintf(`
document.querySelector('[data-testid="expandable-text-button"]').addEventListener('click',function(){
  var el=document.getElementById('JobDetails_AboutTheJob_%s');
  if(el){
    el.style.display='none';
    var body=document.getElementById('jd-body-%s');
    setTimeout(function(){
      if(body){body.textContent=%q;}
      el.style.display='block';
    },%d);
  }
  window._rerenderSimulated=true;
});`, jobID, jobID, fullBody, delayMs)
	return `<!DOCTYPE html><html><body>
<div style="width:1000px;height:3000px">
  <div id="hdr-` + jobID + `" style="display:block;width:800px;height:300px">
    <div aria-label="Company, TestCo." style="display:block;width:200px;height:50px">TestCo</div>
    <p style="display:block;width:500px;height:30px">Test Role</p>
    <a href="/jobs/view/` + jobID + `" style="display:inline-block;width:100px;height:25px">Location</a>
    <button aria-label="Easy Apply to this job" style="display:inline-block;width:140px;height:40px">Easy Apply</button>
  </div>
  <div id="JobDetails_AboutTheJob_` + jobID + `" style="display:block;width:800px;height:600px">
    <h2 style="display:block">About the job</h2>
    <p id="jd-body-` + jobID + `" style="display:block">Description loading, please wait.</p>
    <button data-testid="expandable-text-button" aria-hidden="true" style="display:inline-block;width:60px;height=25px">… more</button>
  </div>
</div>
<script>` + scriptPart + `</script>
</body></html>`
}

// sduiCollapsedPreviewFixture returns SDUI HTML where the description element
// starts with a medium-length preview (> 100 chars but substantially shorter than
// the full JD). After delayMs the element's text is replaced with the full content.
// This simulates a "see more" lazy load: the initial collapsed view already satisfies
// the minimum content threshold, but expansion delivers a longer description.
func sduiCollapsedPreviewFixture(jobID string, delayMs int) string {
	preview := "The position requires demonstrated experience in the field. You will be working on challenging projects with a talented team. We are looking for someone who can contribute immediately."
	fullBody := strings.Repeat("The position requires demonstrated experience and strong skills in the relevant field. ", 5)
	scriptPart := fmt.Sprintf(`setTimeout(function(){
  var el=document.getElementById('jd-body-%s');
  if(el){el.textContent=%q;}
  window._contentArrived=true;
},%d);`, jobID, fullBody, delayMs)
	return `<!DOCTYPE html><html><body>
<div style="width:1000px;height:3000px">
  <div id="hdr-` + jobID + `" style="display:block;width:800px;height:300px">
    <div aria-label="Company, TestCo." style="display:block;width:200px;height:50px">TestCo</div>
    <p style="display:block;width:500px;height:30px">Test Role</p>
    <a href="/jobs/view/` + jobID + `" style="display:inline-block;width:100px;height:25px">Location</a>
    <button aria-label="Easy Apply to this job" style="display:inline-block;width:140px;height:40px">Easy Apply</button>
  </div>
  <div id="JobDetails_AboutTheJob_` + jobID + `" style="display:block;width:800px;height:600px">
    <h2 style="display:block">About the job</h2>
    <p id="jd-body-` + jobID + `" style="display:block">` + preview + `</p>
    <button data-testid="expandable-text-button" aria-hidden="true" style="display:inline-block;width:60px;height=25px">… more</button>
  </div>
</div>
<script>` + scriptPart + `</script>
</body></html>`
}

// sduiShortThenCompleteFixture returns SDUI HTML where the description element
// initially contains a short placeholder (< 100 chars), then after delayMs the
// element's text is replaced with a full description. This simulates description
// content arriving asynchronously after the expand click rather than being
// immediately present in the DOM.
func sduiShortThenCompleteFixture(jobID string, delayMs int) string {
	fullBody := strings.Repeat("The position requires demonstrated experience and strong skills in the relevant field. ", 5)
	scriptPart := fmt.Sprintf(`setTimeout(function(){
  var el=document.getElementById('jd-body-%s');
  if(el){el.textContent=%q;}
  window._contentArrived=true;
},%d);`, jobID, fullBody, delayMs)
	return `<!DOCTYPE html><html><body>
<div style="width:1000px;height:3000px">
  <div id="hdr-` + jobID + `" style="display:block;width:800px;height:300px">
    <div aria-label="Company, TestCo." style="display:block;width:200px;height:50px">TestCo</div>
    <p style="display:block;width:500px;height:30px">Test Role</p>
    <a href="/jobs/view/` + jobID + `" style="display:inline-block;width:100px;height:25px">Location</a>
    <button aria-label="Easy Apply to this job" style="display:inline-block;width:140px;height:40px">Easy Apply</button>
  </div>
  <div id="JobDetails_AboutTheJob_` + jobID + `" style="display:block;width:800px;height:600px">
    <h2 style="display:block">About the job</h2>
    <p id="jd-body-` + jobID + `" style="display:block">Description loading, please wait.</p>
    <button data-testid="expandable-text-button" aria-hidden="true" style="display:inline-block;width:60px;height:25px">… more</button>
  </div>
</div>
<script>` + scriptPart + `</script>
</body></html>`
}

// sduiAlwaysShortFixture returns SDUI HTML where the description element
// permanently contains short placeholder text (< 100 chars). No timer fires,
// so the description never grows to a usable length.
func sduiAlwaysShortFixture(jobID string) string {
	return `<!DOCTYPE html><html><body>
<div style="width:1000px;height:3000px">
  <div id="hdr-` + jobID + `" style="display:block;width:800px;height:300px">
    <div aria-label="Company, TestCo." style="display:block;width:200px;height:50px">TestCo</div>
    <p style="display:block;width:500px;height:30px">Test Role</p>
    <a href="/jobs/view/` + jobID + `" style="display:inline-block;width:100px;height:25px">Location</a>
    <button aria-label="Easy Apply to this job" style="display:inline-block;width:140px;height:40px">Easy Apply</button>
  </div>
  <div id="JobDetails_AboutTheJob_` + jobID + `" style="display:block;width:800px;height:600px">
    <h2 style="display:block">About the job</h2>
    <p style="display:block">Description loading, please wait.</p>
    <button data-testid="expandable-text-button" aria-hidden="true" style="display:inline-block;width:60px;height:25px">… more</button>
  </div>
</div>
</body></html>`
}

// TestReadLinkedInDescription_ShortDescriptionBecomesComplete is a regression for
// the description-readiness gap identified after PR #105. Unlike the SDUI re-render
// race (where identity becomes false while the element is absent), this covers the
// case where identity stays valid throughout but description content arrives
// asynchronously: the first read returns fewer than 100 chars, and a subsequent
// read returns usable content once the DOM is updated.
func TestReadLinkedInDescription_ShortDescriptionBecomesComplete(t *testing.T) {
	br := launchTestBrowser(t)
	jobID := "55598001"
	// Content arrives at 100 ms; the first retry fires at ~400 ms and should see it.
	srv := fixtureServer(t, jobID, "", sduiShortThenCompleteFixture(jobID, 100), "")
	page := navigateAndWait(t, br, srv.URL+"/jobs/view/"+jobID)
	job := linkedInJob{
		ID:      jobID,
		URL:     srv.URL + "/jobs/view/" + jobID,
		Title:   "Test Role",
		Company: "TestCo",
	}
	r, err := readLinkedInDescription(context.Background(), page, job)
	require.NoError(t, err)
	require.True(t, r.IdentityVerified)
	require.Greater(t, len(r.Description), 100)
	require.Contains(t, r.Description, "relevant field")
	arrived, evalErr := page.Eval(`() => window._contentArrived === true`)
	require.NoError(t, evalErr)
	require.True(t, arrived.Value.Bool(), "description content must have arrived via the async update")
}

// TestReadLinkedInDescription_ShortDescriptionNeverComplete verifies that when
// identity stays valid but description content never becomes usable, the function
// returns an errJobDescriptionMissing error with diagnostic length information
// after the 2-second retry deadline. This test necessarily waits for the full
// deadline.
func TestReadLinkedInDescription_ShortDescriptionNeverComplete(t *testing.T) {
	br := launchTestBrowser(t)
	jobID := "55598002"
	srv := fixtureServer(t, jobID, "", sduiAlwaysShortFixture(jobID), "")
	page := navigateAndWait(t, br, srv.URL+"/jobs/view/"+jobID)
	job := linkedInJob{
		ID:      jobID,
		URL:     srv.URL + "/jobs/view/" + jobID,
		Title:   "Test Role",
		Company: "TestCo",
	}
	r, err := readLinkedInDescription(context.Background(), page, job)
	require.Error(t, err)
	require.ErrorIs(t, err, errJobDescriptionMissing)
	require.Contains(t, err.Error(), "description content not available")
	require.Contains(t, err.Error(), "best=")
	_ = r
}

// TestReadLinkedInDescription_CollapsedPreviewExpandsToFull verifies that a
// collapsed preview already exceeding the minimum content threshold is not
// returned prematurely: when expansion delivers a longer description, the reader
// must return the expanded content rather than the initial collapsed snapshot.
func TestReadLinkedInDescription_CollapsedPreviewExpandsToFull(t *testing.T) {
	br := launchTestBrowser(t)
	jobID := "55598003"
	// Preview is ~186 chars (> 100) but full body is ~430 chars. Content arrives
	// at 100 ms; the first retry at ~400 ms should see the expanded description.
	srv := fixtureServer(t, jobID, "", sduiCollapsedPreviewFixture(jobID, 100), "")
	page := navigateAndWait(t, br, srv.URL+"/jobs/view/"+jobID)
	job := linkedInJob{
		ID:      jobID,
		URL:     srv.URL + "/jobs/view/" + jobID,
		Title:   "Test Role",
		Company: "TestCo",
	}
	r, err := readLinkedInDescription(context.Background(), page, job)
	require.NoError(t, err)
	require.True(t, r.IdentityVerified)
	require.Greater(t, len(r.Description), 400) // full body exceeds the 186-char collapsed preview
	require.Contains(t, r.Description, "relevant field")
	arrived, evalErr := page.Eval(`() => window._contentArrived === true`)
	require.NoError(t, evalErr)
	require.True(t, arrived.Value.Bool(), "expanded content must have arrived via the async update")
}

// sduiGoodThenIdentityLostFixture returns SDUI HTML where the description
// element starts with full usable content but is permanently hidden after
// delayMs, causing all subsequent reads to return IdentityVerified=false.
// This simulates a case where an earlier good snapshot must not mask
// subsequent permanent identity loss.
func sduiGoodThenIdentityLostFixture(jobID string, delayMs int) string {
	fullBody := strings.Repeat("The position requires demonstrated experience and strong skills in the relevant field. ", 5)
	scriptPart := fmt.Sprintf(`setTimeout(function(){
  var el=document.getElementById('JobDetails_AboutTheJob_%s');
  if(el){el.style.display='none';}
  window._identityLost=true;
},%d);`, jobID, delayMs)
	return `<!DOCTYPE html><html><body>
<div style="width:1000px;height:3000px">
  <div id="hdr-` + jobID + `" style="display:block;width:800px;height:300px">
    <div aria-label="Company, TestCo." style="display:block;width:200px;height:50px">TestCo</div>
    <p style="display:block;width:500px;height:30px">Test Role</p>
    <a href="/jobs/view/` + jobID + `" style="display:inline-block;width:100px;height:25px">Location</a>
    <button aria-label="Easy Apply to this job" style="display:inline-block;width:140px;height:40px">Easy Apply</button>
  </div>
  <div id="JobDetails_AboutTheJob_` + jobID + `" style="display:block;width:800px;height:600px">
    <h2 style="display:block">About the job</h2>
    <p id="jd-body-` + jobID + `" style="display:block">` + fullBody + `</p>
    <button data-testid="expandable-text-button" aria-hidden="true" style="display:inline-block;width:60px;height:25px">… more</button>
  </div>
</div>
<script>` + scriptPart + `</script>
</body></html>`
}

// TestReadLinkedInDescription_IdentityLostAfterGoodRead verifies that an earlier
// good snapshot does not turn subsequent permanent identity loss into success.
// The T0 read captures a full description, but the element is hidden before the
// first retry, causing all subsequent reads to return IdentityVerified=false.
// The deadline handler must return errLinkedInIdentity, not the bestDesc.
func TestReadLinkedInDescription_IdentityLostAfterGoodRead(t *testing.T) {
	br := launchTestBrowser(t)
	jobID := "55598004"
	// Full description at T0; element hidden at 100 ms (before first retry at ~400 ms).
	srv := fixtureServer(t, jobID, "", sduiGoodThenIdentityLostFixture(jobID, 100), "")
	page := navigateAndWait(t, br, srv.URL+"/jobs/view/"+jobID)
	job := linkedInJob{
		ID:      jobID,
		URL:     srv.URL + "/jobs/view/" + jobID,
		Title:   "Test Role",
		Company: "TestCo",
	}
	r, err := readLinkedInDescription(context.Background(), page, job)
	require.Error(t, err)
	require.ErrorIs(t, err, errLinkedInIdentity, "permanent identity loss must not succeed via a stale good snapshot")
	require.Contains(t, err.Error(), "identity lost at deadline")
	_ = r
}

// TestReadLinkedInDescription_CollapsedPreviewLateExpansion verifies that when a
// preview already exceeds the minimum threshold but full content arrives well
// after the first retry cycle, the reader waits and returns the expanded content.
// One retry cycle does not establish that expansion is complete.
func TestReadLinkedInDescription_CollapsedPreviewLateExpansion(t *testing.T) {
	br := launchTestBrowser(t)
	jobID := "55598005"
	// Preview ~186 chars (> 100); full body (~430 chars) arrives at 700 ms,
	// after the first retry at ~400 ms. The reader must not exit on the first
	// usable read — stability detection (stableCount >= 2) ensures it waits.
	srv := fixtureServer(t, jobID, "", sduiCollapsedPreviewFixture(jobID, 700), "")
	page := navigateAndWait(t, br, srv.URL+"/jobs/view/"+jobID)
	job := linkedInJob{
		ID:      jobID,
		URL:     srv.URL + "/jobs/view/" + jobID,
		Title:   "Test Role",
		Company: "TestCo",
	}
	r, err := readLinkedInDescription(context.Background(), page, job)
	require.NoError(t, err)
	require.True(t, r.IdentityVerified)
	require.Greater(t, len(r.Description), 400, "reader must return expanded content, not the 186-char collapsed preview")
	require.Contains(t, r.Description, "relevant field")
	arrived, evalErr := page.Eval(`() => window._contentArrived === true`)
	require.NoError(t, evalErr)
	require.True(t, arrived.Value.Bool(), "expanded content must have arrived via the async update")
}
