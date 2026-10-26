package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/documents"
)

func fixtureHTML(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("testdata/linkedin/" + name)
	require.NoError(t, err, "fixture %s must exist", name)
	return string(data)
}

const htmlReviewCTAWithCoverFile = `<!DOCTYPE html><html><body>
<div role="dialog" class="jobs-easy-apply-modal" style="padding:16px">
  <button type="button">Review your application</button>
  <div data-test-form-element="">
    <label for="cover-upload">Cover letter (required)</label>
    <input type="file" id="cover-upload" accept=".pdf" aria-required="true" />
  </div>
</div>
</body></html>`

const htmlFinalReviewPage = `<!DOCTYPE html><html><body>
<div role="dialog" class="jobs-easy-apply-modal" style="padding:16px">
  <h2>Review your application</h2>
  <p>Contact info summary</p>
  <button type="button" data-easy-apply-submit-button="">Submit application</button>
</div>
</body></html>`

func rodTestPage(t *testing.T, html string) *rod.Page {
	t.Helper()
	url, err := launcher.New().
		Headless(true).
		Leakless(false).
		NoSandbox(true).
		Launch()
	if err != nil {
		t.Skipf("chrome unavailable for DOM fixture test: %v", err)
	}
	browser := rod.New().ControlURL(url)
	if err := browser.Connect(); err != nil {
		t.Skipf("chrome connect failed: %v", err)
	}
	t.Cleanup(func() { _ = browser.Close() })
	page := browser.MustPage("")
	require.NoError(t, page.SetDocumentContent(html))
	return page
}

func evalScanFields(t *testing.T, page *rod.Page) []formField {
	t.Helper()
	res, err := page.Eval(jsScanFields)
	require.NoError(t, err)
	var fields []formField
	require.NoError(t, json.Unmarshal([]byte(res.Value.String()), &fields))
	return fields
}

func TestJsScanFields_ReviewCTAWithRequiredCoverStillScans(t *testing.T) {
	page := rodTestPage(t, htmlReviewCTAWithCoverFile)

	fields := evalScanFields(t, page)
	require.NotEmpty(t, fields)
	var coverFile *formField
	for i := range fields {
		if fields[i].Type == "file" {
			coverFile = &fields[i]
			break
		}
	}
	require.NotNil(t, coverFile)
	require.Equal(t, documents.KindCoverLetter, coverFile.DocumentKind)
	require.True(t, coverFile.Required)

	final, err := page.Eval(jsLinkedInAtFinalReview)
	require.NoError(t, err)
	require.False(t, final.Value.Bool(), "Review navigation CTA must not count as final review page")
}

func TestJsScanFields_FinalReviewPageSkipsFields(t *testing.T) {
	page := rodTestPage(t, htmlFinalReviewPage)

	fields := evalScanFields(t, page)
	require.Empty(t, fields)

	final, err := page.Eval(jsLinkedInAtFinalReview)
	require.NoError(t, err)
	require.True(t, final.Value.Bool())
}

// TestJsScanFields_NativeDialog_ResumeButton_Detected proves the fix for the production failure
// where jsScanFields returned ResumeFileSlots=0 on LinkedIn's new native-dialog layout.
// Before the fix: returns [] (button not recognized, file-input scan finds nothing).
// After the fix: returns one {type:"file", uploadViaButton:true, documentKind:"resume"}.
func TestJsScanFields_NativeDialog_ResumeButton_Detected(t *testing.T) {
	page := rodTestPage(t, fixtureHTML(t, "native-dialog-resume.html"))

	fields := evalScanFields(t, page)

	var resumeField *formField
	for i := range fields {
		if fields[i].Type == "file" {
			resumeField = &fields[i]
			break
		}
	}
	require.NotNil(t, resumeField, "must detect resume upload slot; got fields: %v", fields)
	require.True(t, resumeField.UploadViaButton, "must set UploadViaButton=true (no <input type=file> in DOM)")
	require.Equal(t, documents.KindResume, resumeField.DocumentKind)
	require.True(t, resumeField.Required, "section label has asterisk — required must be detected from label text")
}

// TestJsScanFields_NativeDialog_FinalReview proves jsLinkedInAtFinalReview detects
// the final review page when it lives inside a <dialog open> (no role="dialog").
// Before the fix: returns false (shells querySelectorAll misses <dialog>).
// After the fix: returns true.
func TestJsScanFields_NativeDialog_FinalReview(t *testing.T) {
	page := rodTestPage(t, fixtureHTML(t, "native-dialog-final-review.html"))

	final, err := page.Eval(jsLinkedInAtFinalReview)
	require.NoError(t, err)
	require.True(t, final.Value.Bool(), "must detect final review page inside native <dialog open>")

	fields := evalScanFields(t, page)
	require.Empty(t, fields, "final review page must return no fields to fill")
}

// TestJsScanFields_NativeDialog_ContactPage_ScopeToDialog proves that jsScanFields
// scopes its search to the dialog when the native-dialog layout is used, excluding
// controls rendered outside the dialog.
func TestJsScanFields_NativeDialog_ContactPage_ScopeToDialog(t *testing.T) {
	// This fixture has all fields pre-filled (matching captures), so jsScanFields
	// returns no actionable fields. The key assertion is that the scan does not
	// raise errors and does not pick up the job-details content below the dialog.
	page := rodTestPage(t, fixtureHTML(t, "native-dialog-contact.html"))

	fields := evalScanFields(t, page)
	// All contact fields already have values — scan correctly returns empty.
	// Verify: no fields leaked in from the job-details section outside the dialog.
	for _, f := range fields {
		require.NotEqual(t, "Software Engineer", f.Question,
			"job details section outside dialog must not be scoped into field scan")
	}
}

// TestJsScanFields_NativeDialog_NoDoubleCount verifies that when a visible
// <input type="file"> already exists inside the resume section (old layout),
// the button-scan block does NOT add a duplicate entry.
func TestJsScanFields_NativeDialog_NoDoubleCount(t *testing.T) {
	// Resume section has BOTH a visible file input AND an "Upload resume" button.
	const html = `<!DOCTYPE html><html><body>
<dialog data-testid="dialog" aria-labelledby="h1" open="">
  <h1 id="h1">Apply</h1>
  <div data-sdui-screen="com.linkedin.sdui.flagshipnav.jobs.easyapply.EasyApply">
    <div>
      <label for="res">Resume</label>
      <input type="file" id="res" accept=".pdf" style="display:block;width:200px;height:40px" />
      <button type="button">Upload resume</button>
    </div>
    <footer><button type="button"><span>Next</span></button></footer>
  </div>
</dialog>
</body></html>`
	page := rodTestPage(t, html)
	fields := evalScanFields(t, page)

	var fileFields []formField
	for _, f := range fields {
		if f.Type == "file" {
			fileFields = append(fileFields, f)
		}
	}
	require.Len(t, fileFields, 1, "must have exactly one file field when visible input and button coexist")
	require.False(t, fileFields[0].UploadViaButton, "visible input wins; UploadViaButton must be false")
}

// TestJsScanFields_CaptchaTextarea_Excluded verifies that g-recaptcha-response
// textareas are excluded from the text field scan.
func TestJsScanFields_CaptchaTextarea_Excluded(t *testing.T) {
	const html = `<!DOCTYPE html><html><body>
<dialog data-testid="dialog" aria-labelledby="h1" open="">
  <h1 id="h1">Apply</h1>
  <div data-sdui-screen="com.linkedin.sdui.flagshipnav.jobs.easyapply.EasyApply">
    <div>
      <label for="cover-msg">Cover message</label>
      <textarea id="cover-msg" style="display:block;width:300px;height:100px"></textarea>
    </div>
    <!-- reCAPTCHA injected textarea — must be excluded -->
    <textarea name="g-recaptcha-response" style="display:none"></textarea>
    <footer><button type="button"><span>Next</span></button></footer>
  </div>
</dialog>
</body></html>`
	page := rodTestPage(t, html)
	fields := evalScanFields(t, page)

	for _, f := range fields {
		require.NotContains(t, f.Question, "g-recaptcha",
			"g-recaptcha-response textarea must not appear in field scan")
		require.NotEqual(t, "g-recaptcha-response", f.ID,
			"g-recaptcha-response must be excluded from text scan")
	}
}

// TestJsScanFields_LegacyLinkedIn_Regression verifies that the old .jobs-easy-apply-modal
// layout still works after the native-dialog container resolver is added.
func TestJsScanFields_LegacyLinkedIn_Regression(t *testing.T) {
	page := rodTestPage(t, htmlReviewCTAWithCoverFile)

	fields := evalScanFields(t, page)
	require.NotEmpty(t, fields)
	var coverFile *formField
	for i := range fields {
		if fields[i].Type == "file" {
			coverFile = &fields[i]
			break
		}
	}
	require.NotNil(t, coverFile)
	require.Equal(t, documents.KindCoverLetter, coverFile.DocumentKind)
	require.True(t, coverFile.Required)
}

// TestJsScanFields_NativeDialog_ResumeAtPositions verifies that jsScanFields detects
// the upload-via-button resume field regardless of where it appears in the form.
func TestJsScanFields_NativeDialog_ResumeAtPositions(t *testing.T) {
	makeDialog := func(extra string) string {
		return fmt.Sprintf(`<!DOCTYPE html><html><body>
<dialog data-testid="dialog" open="">
  <div data-sdui-screen="com.linkedin.sdui.flagshipnav.jobs.easyapply.EasyApply">
    %s
    <div id="resume-section">
      <label>Resume <span aria-hidden="true">*</span></label>
      <div>
        <input type="radio" name="resume-select" id="r-lib1" value="lib1">
        <label for="r-lib1">my_resume_2026.pdf</label>
      </div>
      <button type="button"><span><span>Upload resume</span></span></button>
    </div>
    <footer><button type="button"><span>Next</span></button></footer>
  </div>
</dialog>
</body></html>`, extra)
	}

	cases := []struct {
		name  string
		extra string
	}{
		{"position_first", ""},
		{"position_after_text_field", `<div data-test-form-element="">
          <label for="location">City</label>
          <input id="location" type="text" value="Berlin" style="display:block;width:200px;height:40px">
        </div>`},
		{"position_after_multiple_fields", `<div data-test-form-element="">
          <label for="name">Full name</label>
          <input id="name" type="text" value="Alex Smith" style="display:block;width:200px;height:40px">
        </div>
        <fieldset>
          <legend>Years of experience</legend>
          <input type="radio" name="yoe" id="yoe1" value="1-3" checked>
          <label for="yoe1">1–3 years</label>
        </fieldset>`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			page := rodTestPage(t, makeDialog(tc.extra))
			fields := evalScanFields(t, page)

			var resumeField *formField
			for i := range fields {
				if fields[i].Type == "file" {
					resumeField = &fields[i]
					break
				}
			}
			require.NotNil(t, resumeField, "must detect resume upload slot at position %s; got fields: %v", tc.name, fields)
			require.True(t, resumeField.UploadViaButton, "must set UploadViaButton=true")
			require.True(t, resumeField.Required, "must detect required from asterisk label")
			require.Equal(t, documents.KindResume, resumeField.DocumentKind)
		})
	}
}

// TestLinkedInFinalReviewResumeCheck_Match verifies that jsLinkedInFinalReviewResumeCheck
// returns verified=true when the review section shows the expected filename.
// Uses a navigated page (fixtureServer) to confirm the JS works via HTTP, not just SetDocumentContent.
func TestLinkedInFinalReviewResumeCheck_Match(t *testing.T) {
	br := launchTestBrowser(t)
	const html = `<!DOCTYPE html><html><body>
<dialog data-testid="dialog" open="">
  <div data-sdui-screen="com.linkedin.sdui.flagshipnav.jobs.easyapply.EasyApply">
    <h2>Review your application</h2>
    <section class="review-section artdeco-card">
      <h3>Resume</h3>
      <p id="review-resume-name">my_tailored_resume.pdf</p>
    </section>
    <button data-easy-apply-submit-button="" type="button"><span>Submit application</span></button>
  </div>
</dialog>
</body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, html)
	}))
	t.Cleanup(srv.Close)
	page := navigateAndWait(t, br, srv.URL)

	res, err := page.Eval(jsLinkedInFinalReviewResumeCheck, "my_tailored_resume.pdf")
	require.NoError(t, err)
	require.True(t, res.Value.Get("verified").Bool(),
		"matching filename must return verified=true; full result: %s", res.Value.String())
}

// TestLinkedInFinalReviewResumeCheck_Mismatch verifies that jsLinkedInFinalReviewResumeCheck
// returns verified=false when the review section shows a different filename.
func TestLinkedInFinalReviewResumeCheck_Mismatch(t *testing.T) {
	br := launchTestBrowser(t)
	const html = `<!DOCTYPE html><html><body>
<dialog data-testid="dialog" open="">
  <div data-sdui-screen="com.linkedin.sdui.flagshipnav.jobs.easyapply.EasyApply">
    <h2>Review your application</h2>
    <section class="review-section artdeco-card">
      <h3>Resume</h3>
      <p id="review-resume-name">wrong_resume.pdf</p>
    </section>
    <button data-easy-apply-submit-button="" type="button"><span>Submit application</span></button>
  </div>
</dialog>
</body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, html)
	}))
	t.Cleanup(srv.Close)
	page := navigateAndWait(t, br, srv.URL)

	res, err := page.Eval(jsLinkedInFinalReviewResumeCheck, "my_tailored_resume.pdf")
	require.NoError(t, err)
	require.False(t, res.Value.Get("verified").Bool(),
		"mismatched filename must return verified=false; full result: %s", res.Value.String())
}

// TestScanApplyStepsForPrepare_NativeDialog verifies the three key paths in
// scanApplyStepsForPrepare for the LinkedIn native-dialog layout using a
// single-step final-review form. This avoids dependence on CDP file-chooser
// interception (which requires a trusted user gesture) while still covering
// all three code paths through linkedInVerifyFinalReviewResume.
//
// The upload-mismatch detection path (errUploadIdentityUnverifiable) is tested
// separately via TestLinkedInWaitForResumeAccepted_IdentityUnverifiable.
//
//  1. upload_correct       — review confirms expected filename → prepareScanComplete=true
//  2. upload_wrong_file    — review shows wrong filename → hold, prepareScanComplete=false
//  3. final_review_mismatch — review shows old filename → hold, prepareScanComplete=false
func TestScanApplyStepsForPrepare_NativeDialog(t *testing.T) {
	br := launchTestBrowser(t)

	const jobID = "scan-native-job1"

	// buildReviewForm returns a single-step final-review dialog showing reviewResumeName.
	buildReviewForm := func(reviewResumeName string) string {
		return `<!DOCTYPE html><html><body>
<dialog data-testid="dialog" data-job-id="` + jobID + `" open="">
  <div data-sdui-screen="com.linkedin.sdui.flagshipnav.jobs.easyapply.EasyApply">
    <h2>Review your application</h2>
    <section class="review-section artdeco-card">
      <h3>Resume</h3>
      <p id="review-resume-name">` + reviewResumeName + `</p>
    </section>
    <button data-easy-apply-submit-button="" type="button"><span>Submit application</span></button>
  </div>
</dialog>
</body></html>`
	}

	cases := []struct {
		name         string
		reviewName   string
		wantComplete bool
		wantHold     bool
	}{
		{
			name:         "upload_correct",
			reviewName:   "my_tailored_resume.pdf",
			wantComplete: true,
			wantHold:     false,
		},
		{
			name:         "upload_wrong_file",
			reviewName:   "wrong_resume.pdf",
			wantComplete: false,
			wantHold:     true,
		},
		{
			name:         "final_review_mismatch",
			reviewName:   "old_resume.pdf",
			wantComplete: false,
			wantHold:     true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			html := buildReviewForm(tc.reviewName)
			srv := fixtureServer(t, jobID, "", html, "")
			page := navigateAndWait(t, br, srv.URL+"/jobs/view/"+jobID)

			tmpFile := filepath.Join(t.TempDir(), "my_tailored_resume.pdf")
			require.NoError(t, os.WriteFile(tmpFile, []byte("%PDF-1.4"), 0644))

			b := &Bot{cfg: Config{}, seenCache: newJobSeenCache()}
			b.cfg.Settings.GenerateNewResumeDocs = true
			lazy := &lazyDocGen{
				b:                 b,
				job:               linkedInJob{ID: jobID, URL: srv.URL + "/jobs/view/" + jobID},
				reviewPrepareOnly: true,
				ctx:               context.Background(),
			}
			// Pre-seed the resume path so get()/peek() return tmpFile without LLM.
			lazy.once.Do(func() { lazy.resume = tmpFile })

			err := b.scanApplyStepsForPrepare(context.Background(), page, lazy)

			if tc.wantComplete {
				require.NoError(t, err, "scan must complete without error")
				require.True(t, lazy.prepareScanComplete, "prepareScanComplete must be true")
				require.Empty(t, lazy.holdReason, "holdReason must be empty on success")
			} else {
				require.Error(t, err, "scan must return an error when blocked")
				require.False(t, lazy.prepareScanComplete, "prepareScanComplete must be false on failure")
				require.NotEmpty(t, lazy.holdReason, "holdReason must be set")
			}
		})
	}
}

// TestScanApplyStepsForPrepare_UploadNavigateFinalReview is a connected integration
// test that exercises the full upload → navigate → final-review path through
// scanApplyStepsForPrepare. It uses a 3-step native dialog served via fixtureServer
// so that verifyJobIdentity accepts the localhost URL:
//
//   - Step 1: resume section with an upload button; Rod's trusted .Click() triggers
//     the CDP file chooser; the change handler creates a checked radio with the
//     uploaded filename and sets window._uploadedResumeName.
//   - Step 2: a text field (filler step, skipped in reviewPrepareOnly mode).
//   - Step 3: final-review heading + ReviewSection + submit button; the "Next"
//     JS on step 2→3 propagates window._uploadedResumeName into the review section.
//
// Sub-tests:
//  1. correct_upload   — radio gets the real filename → prepareScanComplete=true
//  2. wrong_file_from_upload — change handler hard-codes "wrong_resume.pdf" →
//     errUploadIdentityUnverifiable → holdReason set, prepareScanComplete=false
func TestScanApplyStepsForPrepare_UploadNavigateFinalReview(t *testing.T) {
	br := launchTestBrowser(t)

	const jobID = "scan-upload-job1"

	// buildHTML creates the 3-step dialog HTML. When wrongFile is true the change
	// handler hard-codes "wrong_resume.pdf" instead of using f.files[0].name.
	buildHTML := func(wrongFile bool) string {
		changeBody := `
    var name = f.files[0] ? f.files[0].name : 'unknown.pdf';
    window._uploadedResumeName = name;
    var wrap = document.createElement('div');
    var radio = document.createElement('input');
    radio.type = 'radio';
    radio.name = 'resume-select';
    radio.id = 'r-uploaded';
    radio.checked = true;
    var lbl = document.createElement('label');
    lbl.htmlFor = 'r-uploaded';
    lbl.textContent = name;
    wrap.appendChild(radio);
    wrap.appendChild(lbl);
    document.getElementById('resume-section').insertBefore(wrap, document.getElementById('upload-btn'));
    f.remove();`
		if wrongFile {
			changeBody = `
    window._uploadedResumeName = 'wrong_resume.pdf';
    var wrap = document.createElement('div');
    var radio = document.createElement('input');
    radio.type = 'radio';
    radio.name = 'resume-select';
    radio.id = 'r-uploaded';
    radio.checked = true;
    var lbl = document.createElement('label');
    lbl.htmlFor = 'r-uploaded';
    lbl.textContent = 'wrong_resume.pdf';
    wrap.appendChild(radio);
    wrap.appendChild(lbl);
    document.getElementById('resume-section').insertBefore(wrap, document.getElementById('upload-btn'));
    f.remove();`
		}
		return `<!DOCTYPE html><html><body>
<dialog data-testid="dialog" data-job-id="` + jobID + `" open="">
  <div data-sdui-screen="com.linkedin.sdui.flagshipnav.jobs.easyapply.EasyApply">

    <!-- Step 1: resume upload (visible) -->
    <div id="step-1">
      <div id="resume-section">
        <label>Resume <span aria-hidden="true">*</span></label>
        <button type="button" id="upload-btn">
          <span><span>Upload resume</span></span>
        </button>
      </div>
      <footer>
        <button type="button" id="next-1"><span>Next</span></button>
      </footer>
    </div>

    <!-- Step 2: text field (hidden) -->
    <div id="step-2" style="display:none">
      <div data-test-form-element="">
        <label for="city">City</label>
        <input id="city" type="text" style="display:block;width:200px;height:40px">
      </div>
      <footer>
        <button type="button" id="next-2"><span>Next</span></button>
      </footer>
    </div>

    <!-- Step 3: final review (hidden) -->
    <div id="step-3" style="display:none">
      <h2>Review your application</h2>
      <section data-component-type="ReviewSection">
        <h3>Resume</h3>
        <p id="review-resume-name"></p>
      </section>
      <button data-easy-apply-submit-button="" id="submit-btn" onclick="window._submitClicks=(window._submitClicks||0)+1;">Submit application</button>
    </div>

  </div>
</dialog>
<script>
document.getElementById('upload-btn').addEventListener('click', function() {
  var f = document.createElement('input');
  f.type = 'file';
  f.style.display = 'none';
  f.addEventListener('change', function() {` + changeBody + `
  });
  document.body.appendChild(f);
  f.click();
});
document.getElementById('next-1').addEventListener('click', function() {
  document.getElementById('step-1').style.display = 'none';
  document.getElementById('step-2').style.display = '';
});
document.getElementById('next-2').addEventListener('click', function() {
  document.getElementById('step-2').style.display = 'none';
  document.getElementById('step-3').style.display = '';
  document.getElementById('review-resume-name').textContent = window._uploadedResumeName || '';
});
</script>
</body></html>`
	}

	cases := []struct {
		name         string
		wrongFile    bool
		wantComplete bool
	}{
		{name: "correct_upload", wrongFile: false, wantComplete: true},
		{name: "wrong_file_from_upload", wrongFile: true, wantComplete: false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			html := buildHTML(tc.wrongFile)
			srv := fixtureServer(t, jobID, "", html, "")
			page := navigateAndWait(t, br, srv.URL+"/jobs/view/"+jobID)

			tmpFile := filepath.Join(t.TempDir(), "my_tailored_resume.pdf")
			require.NoError(t, os.WriteFile(tmpFile, []byte("%PDF-1.4"), 0644))

			b := &Bot{cfg: Config{}, seenCache: newJobSeenCache()}
			b.cfg.Settings.GenerateNewResumeDocs = true
			lazy := &lazyDocGen{
				b:                 b,
				job:               linkedInJob{ID: jobID, URL: srv.URL + "/jobs/view/" + jobID},
				reviewPrepareOnly: true,
				ctx:               context.Background(),
			}
			lazy.once.Do(func() { lazy.resume = tmpFile })

			err := b.scanApplyStepsForPrepare(context.Background(), page, lazy)

			// Assert zero Submit clicks regardless of outcome.
			submitClicks, evalErr := page.Eval(`() => window._submitClicks || 0`)
			require.NoError(t, evalErr)
			require.EqualValues(t, 0, submitClicks.Value.Num(), "Submit must not be clicked during preparation scan")

			if tc.wantComplete {
				require.NoError(t, err, "correct upload must complete without error")
				require.True(t, lazy.prepareScanComplete, "prepareScanComplete must be true on success")
				require.Empty(t, lazy.holdReason, "holdReason must be empty on success")

				// Verify file was delivered and the correct filename is selected.
				acceptRes, aErr := page.Eval(jsLinkedInCheckResumeAccepted, "my_tailored_resume.pdf")
				require.NoError(t, aErr)
				require.True(t, acceptRes.Value.Get("accepted").Bool(), "uploaded radio must be checked")
				require.True(t, acceptRes.Value.Get("filenameVisible").Bool(), "uploaded filename must match")

				// Verify we reached and passed the final-review step.
				frRes, frErr := page.Eval(jsLinkedInAtFinalReview)
				require.NoError(t, frErr)
				require.True(t, frRes.Value.Bool(), "page must be at final review after preparation scan")
			} else {
				require.False(t, lazy.prepareScanComplete, "prepareScanComplete must be false when upload identity unverifiable")
				require.NotEmpty(t, lazy.holdReason, "holdReason must be set when upload identity unverifiable")
			}
		})
	}
}
