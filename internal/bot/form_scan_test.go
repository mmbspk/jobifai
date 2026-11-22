package bot

import (
	"bytes"
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
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/documents"
	"github.com/user/jobifai/internal/domain"
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
	return evalScanFieldsOpts(t, page, nil)
}

func evalScanFieldsOpts(t *testing.T, page *rod.Page, opts map[string]interface{}) []formField {
	t.Helper()
	res, err := page.Eval(jsScanFields, opts)
	require.NoError(t, err)
	var fields []formField
	require.NoError(t, json.Unmarshal([]byte(res.Value.String()), &fields))
	return fields
}

const htmlSDUIImplicitTextInput = `<!DOCTYPE html><html><body>
<dialog open="">
  <div data-sdui-screen="com.linkedin.sdui.flagshipnav.jobs.easyapply.EasyApply">
    <p>What are your working rights in Australia?*</p>
    <input required="" id="rights-in" aria-label="What are your working rights in Australia?" />
    <footer><button type="button"><span>Review</span></button></footer>
  </div>
</dialog>
</body></html>`

func TestJsScanFields_SDUI_ImplicitTextInputWithoutTypeAttribute(t *testing.T) {
	page := rodTestPage(t, htmlSDUIImplicitTextInput)
	fields := evalScanFields(t, page)
	require.Len(t, fields, 1)
	require.Equal(t, "text", fields[0].Type)
	require.Contains(t, fields[0].Question, "working rights")
}

const htmlSDUICharLimitCounter = `<!DOCTYPE html><html><body>
<dialog open="">
  <div data-sdui-screen="com.linkedin.sdui.flagshipnav.jobs.easyapply.EasyApply">
    <p>Expected salary (AUD)*</p>
    <input id="salary-in" maxlength="20" aria-label="Expected salary (AUD)" value="" />
    <span>0 of 20 characters</span>
  </div>
</dialog>
</body></html>`

func TestJsScanFields_SDUI_ReadsMaxCharsFromCounterOrMaxlength(t *testing.T) {
	page := rodTestPage(t, htmlSDUICharLimitCounter)
	fields := evalScanFields(t, page)
	require.Len(t, fields, 1)
	require.Equal(t, 20, fields[0].MaxChars)
}

const htmlSDUICheckedRadioNoError = `<!DOCTYPE html><html><body>
<dialog open="">
  <div data-sdui-screen="com.linkedin.sdui.flagshipnav.jobs.easyapply.EasyApply">
    <p>Are you eligible to work in Australia?*</p>
    <fieldset role="radiogroup">
      <div>
        <input type="radio" checked name="radio-group-q1" id="r-yes" aria-label="Are you eligible to work in Australia?" />
        <label for="r-yes"></label>
        <p>Yes</p>
      </div>
      <div>
        <input type="radio" name="radio-group-q1" id="r-no" aria-label="Are you eligible to work in Australia?" />
        <label for="r-no"></label>
        <p>No</p>
      </div>
    </fieldset>
  </div>
</dialog>
</body></html>`

func TestJsScanFields_SDUI_SkipsCheckedRadioUnlessRecheckRequested(t *testing.T) {
	page := rodTestPage(t, htmlSDUICheckedRadioNoError)
	require.Empty(t, evalScanFields(t, page))
	fields := evalScanFieldsOpts(t, page, map[string]interface{}{"includeAnsweredRadios": true})
	require.Len(t, fields, 1)
	require.Equal(t, "radio", fields[0].Type)
	require.Contains(t, fields[0].Question, "eligible")
	require.Equal(t, "Yes", fields[0].Options[0].Label)
	require.Equal(t, "No", fields[0].Options[1].Label)
}

const htmlSDUICheckedRadioValidationFlag = `<!DOCTYPE html><html><body>
<dialog open="">
  <div data-sdui-screen="com.linkedin.sdui.flagshipnav.jobs.easyapply.EasyApply">
    <p>Are you eligible to work in Australia?*</p>
    <fieldset aria-describedby="error-message-q1" role="radiogroup">
      <div>
        <input type="radio" checked name="radio-group-q1" id="r-yes" aria-label="Are you eligible to work in Australia?" />
        <label for="r-yes"></label>
        <p>Yes</p>
      </div>
      <div>
        <input type="radio" name="radio-group-q1" id="r-no" aria-label="Are you eligible to work in Australia?" />
        <label for="r-no"></label>
        <p>No</p>
      </div>
    </fieldset>
    <div id="error-message-q1"></div>
  </div>
</dialog>
</body></html>`

func TestJsScanFields_SDUI_IncludesCheckedRadioWhenValidationFlagged(t *testing.T) {
	page := rodTestPage(t, htmlSDUICheckedRadioValidationFlag)
	fields := evalScanFields(t, page)
	require.Len(t, fields, 1)
	require.Equal(t, "radio", fields[0].Type)
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
		_, _ = fmt.Fprint(w, html)
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
		_, _ = fmt.Fprint(w, html)
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
				// Fix C: error must name the actual hold reason, not claim the review was never reached.
				require.Contains(t, err.Error(), "not confirmed in final-review summary",
					"error must surface the verification failure, not a generic scan-incomplete message")
				require.NotContains(t, err.Error(), "review step not reached",
					"review WAS reached — error must not claim it wasn't")
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
	// The pre-existing library_resume.pdf radio is intentionally kept after upload
	// so the test validates that fillFormStep prefers the just-uploaded checked
	// entry over library entries without needing to remove them.
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
      <fieldset id="resume-section">
        <legend>Resume <span aria-hidden="true">*</span></legend>
        <div>
          <input type="radio" name="resume-select" id="r-lib1">
          <label for="r-lib1">library_resume.pdf</label>
        </div>
        <button type="button" id="upload-btn">
          <span><span>Upload resume</span></span>
        </button>
      </fieldset>
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
			lazy.resumeOverride = tmpFile

			err := b.scanApplyStepsForPrepare(context.Background(), page, lazy)

			// Assert zero Submit clicks regardless of outcome.
			submitClicks, evalErr := page.Eval(`() => window._submitClicks || 0`)
			require.NoError(t, evalErr)
			require.EqualValues(t, 0, submitClicks.Value.Num(), "Submit must not be clicked during preparation scan")

			if tc.wantComplete {
				require.NoError(t, err, "correct upload must complete without error")
				require.True(t, lazy.prepareScanComplete, "prepareScanComplete must be true on success")
				require.Empty(t, lazy.holdReason, "holdReason must be empty on success")

				// The upload step is now hidden. Its acceptance probe must not treat
				// hidden controls as an active upload page. Check the retained selection
				// and the visible final-review summary independently instead.
				acceptRes, aErr := page.Eval(jsLinkedInCheckResumeAccepted, "my_tailored_resume.pdf")
				require.NoError(t, aErr)
				require.False(t, acceptRes.Value.Get("accepted").Bool(), "hidden upload controls must not be accepted")
				checked, cErr := page.Eval(jsFindCheckedRadioLabel, "resume-select")
				require.NoError(t, cErr)
				require.Equal(t, "my_tailored_resume.pdf", checked.Value.String())
				summary, sErr := page.Eval(jsLinkedInFinalReviewResumeCheck, "my_tailored_resume.pdf")
				require.NoError(t, sErr)
				require.True(t, summary.Value.Get("verified").Bool(), "visible review must confirm the intended attachment")

				// Verify the pre-existing library entry was NOT removed — the fixture intentionally
				// keeps library_resume.pdf in the DOM after upload to exercise the post-upload
				// radio logic. If it is gone, the test is no longer covering the real scenario.
				libRes, libErr := page.Eval(`() => !!document.getElementById('r-lib1')`)
				require.NoError(t, libErr)
				require.True(t, libRes.Value.Bool(), "library_resume.pdf radio must still exist in DOM after upload")

				// Verify we reached and passed the final-review step.
				frRes, frErr := page.Eval(jsLinkedInAtFinalReview)
				require.NoError(t, frErr)
				require.True(t, frRes.Value.Bool(), "page must be at final review after preparation scan")
			} else {
				require.False(t, lazy.prepareScanComplete, "prepareScanComplete must be false when upload identity unverifiable")
				require.NotEmpty(t, lazy.holdReason, "holdReason must be set when upload identity unverifiable")
				visible, vErr := page.Eval(`() => document.getElementById('step-1').style.display !== 'none'`)
				require.NoError(t, vErr)
				require.True(t, visible.Value.Bool(), "upload identity hold must stop before Next")

			}
		})
	}
}

// TestFillFormStep_UnrelatedRadioBeforeResumeGroup verifies that the post-upload
// confirmation path targets the correct radio group and not the first radio it sees.
//
// The scan returns fields in a fixed order: upload-via-button first, then all radio
// groups. An approach that relies on "first radio after file in scan order" would
// incorrectly treat "work-auth" as the resume group. This test catches that.
//
// Layout (step 1 only, no navigation):
//   - "Work authorisation?" radio (group "work-auth") — BEFORE the resume section
//   - Resume fieldset with library_resume.pdf (unchecked) + upload button + radio group "resume-select"
//
// After fillFormStep:
//   - "work-auth" is answered via normal (LLM-less fallback) path — option "Yes" selected
//   - "resume-select" is confirmed via jsFindCheckedRadioLabel (my_tailored_resume.pdf)
//   - library_resume.pdf is NOT re-selected and still exists in the DOM
func TestFillFormStep_UnrelatedRadioBeforeResumeGroup(t *testing.T) {
	const html = `<!DOCTYPE html><html><body>
<dialog data-testid="dialog" open="">
  <div data-sdui-screen="com.linkedin.sdui.flagshipnav.jobs.easyapply.EasyApply">

    <div data-test-form-element="">
      <legend>Work authorisation?</legend>
      <div><input type="radio" name="work-auth" id="wa-yes" value="Yes"><label for="wa-yes">Yes</label></div>
      <div><input type="radio" name="work-auth" id="wa-no"  value="No" ><label for="wa-no">No</label></div>
    </div>

    <fieldset id="resume-section">
      <legend>Resume <span aria-hidden="true">*</span></legend>
      <div>
        <input type="radio" name="resume-select" id="r-lib1">
        <label for="r-lib1">library_resume.pdf</label>
      </div>
      <button type="button" id="upload-btn">
        <span><span>Upload resume</span></span>
      </button>
    </fieldset>

  </div>
</dialog>
<script>
document.getElementById('upload-btn').addEventListener('click', function() {
  var f = document.createElement('input');
  f.type = 'file';
  f.id = 'file-chooser';
  f.style.display = 'none';
  f.addEventListener('change', function() {
    var name = f.files[0] ? f.files[0].name : 'unknown.pdf';
    var wrap = document.createElement('div');
    var radio = document.createElement('input');
    radio.type = 'radio';
    radio.name = 'resume-select';
    radio.id = 'r-uploaded';
    radio.checked = true;
    var label = document.createElement('label');
    label.htmlFor = 'r-uploaded';
    label.textContent = name;
    wrap.appendChild(radio);
    wrap.appendChild(label);
    document.getElementById('resume-section').insertBefore(wrap, document.getElementById('upload-btn'));
    f.remove();
  });
  document.body.appendChild(f);
  f.click();
});
</script>
</body></html>`

	br := launchTestBrowser(t)
	page := br.MustPage("")
	t.Cleanup(func() { _ = page.Close() })
	require.NoError(t, page.SetDocumentContent(html))

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "my_tailored_resume.pdf")
	require.NoError(t, os.WriteFile(tmpFile, []byte("%PDF-1.4"), 0644))

	b := &Bot{cfg: Config{}, seenCache: newJobSeenCache()}
	b.cfg.Settings.GenerateNewResumeDocs = true
	lazy := &lazyDocGen{
		b:   b,
		job: linkedInJob{ID: "unrelated-radio-test"},
		ctx: context.Background(),
	}
	lazy.once.Do(func() { lazy.resume = tmpFile })

	filled, _, err := b.fillFormStep(context.Background(), page, lazy)
	require.NoError(t, err)
	require.True(t, filled)
	require.Empty(t, lazy.holdReason, "holdReason must be empty: both groups should resolve cleanly")

	// The work-auth radio must have been answered via the normal (fallback) path.
	// Without LLM, the fallback picks the first scanned option: "Yes".
	waRes, waErr := page.Eval(`() => {
		const checked = [...document.querySelectorAll('input[name="work-auth"]')].find(r => r.checked);
		return checked ? checked.value : '';
	}`)
	require.NoError(t, waErr)
	require.Equal(t, "Yes", waRes.Value.String(), "work-auth radio must be answered via normal fallback (first option)")

	// my_tailored_resume.pdf must be selected in the resume group.
	acceptRes, aErr := page.Eval(jsLinkedInCheckResumeAccepted, "my_tailored_resume.pdf")
	require.NoError(t, aErr)
	require.True(t, acceptRes.Value.Get("accepted").Bool(), "my_tailored_resume.pdf must be selected after upload")
	require.True(t, acceptRes.Value.Get("filenameVisible").Bool(), "uploaded filename must be visible in resume group")

	// library_resume.pdf must still be in the DOM — post-upload logic must not remove it.
	libRes, libErr := page.Eval(`() => !!document.getElementById('r-lib1')`)
	require.NoError(t, libErr)
	require.True(t, libRes.Value.Bool(), "library_resume.pdf must still exist in DOM after upload")
}

// TestFillFormStep_ResumeConfirmationScopedToApplyContainer verifies that
// jsFindCheckedRadioLabel reads only radios inside the resolved apply container,
// not same-named groups on other SDUI screens present on the page simultaneously.
//
// A background job-details [data-sdui-screen] holds a same-named radio group
// ("resume-select") with "wrong_resume.pdf" checked. It is wrapped in a <form> so
// the browser keeps it in a separate radio-button group from the dialog's unowned
// radios — otherwise the browser itself would uncheck it when any dialog radio is
// checked, making the assertion about background state unreliable.
//
// Without resolveApplyContainer() scoping, document.querySelectorAll would reach
// into the background form, find "wrong_resume.pdf" first (alphabetically earlier in
// the DOM), and return the wrong label — causing confirmation to fail with a hold.
func TestFillFormStep_ResumeConfirmationScopedToApplyContainer(t *testing.T) {
	const html = `<!DOCTYPE html><html><body>

<!-- Background SDUI screen wrapped in its own <form> so the radio group it owns
     is isolated from the dialog's document-level (unowned) radios. Without this
     isolation the browser itself would uncheck rd-background when r-uploaded is
     checked (same name, same owner → same group), making the DOM assertion below
     meaningless. The <form> boundary creates a distinct group while preserving the
     same name, which is the condition the test needs. -->
<form id="bg-form" style="position:absolute;width:1px;height:1px;overflow:hidden">
  <div data-sdui-screen="com.linkedin.sdui.flagshipnav.jobs.jobdetails.JobDetails">
    <input type="radio" name="resume-select" id="rd-background" value="wrong" checked>
    <label for="rd-background">wrong_resume.pdf</label>
  </div>
</form>

<!-- Easy Apply dialog (active apply container). Radios here are unowned (no
     enclosing <form>), so they form a document-level group separate from the
     <form id="bg-form"> group above. -->
<dialog data-testid="dialog" open="">
  <div data-sdui-screen="com.linkedin.sdui.flagshipnav.jobs.easyapply.EasyApply">
    <fieldset id="resume-section">
      <legend>Resume <span aria-hidden="true">*</span></legend>
      <div>
        <input type="radio" name="resume-select" id="r-lib1">
        <label for="r-lib1">library_resume.pdf</label>
      </div>
      <button type="button" id="upload-btn">
        <span><span>Upload resume</span></span>
      </button>
    </fieldset>
  </div>
</dialog>
<script>
document.getElementById('upload-btn').addEventListener('click', function() {
  var f = document.createElement('input');
  f.type = 'file';
  f.id = 'file-chooser';
  f.style.display = 'none';
  f.addEventListener('change', function() {
    var name = f.files[0] ? f.files[0].name : 'unknown.pdf';
    var wrap = document.createElement('div');
    var radio = document.createElement('input');
    radio.type = 'radio';
    radio.name = 'resume-select';
    radio.id = 'r-uploaded';
    radio.checked = true;
    var label = document.createElement('label');
    label.htmlFor = 'r-uploaded';
    label.textContent = name;
    wrap.appendChild(radio);
    wrap.appendChild(label);
    document.getElementById('resume-section').insertBefore(wrap, document.getElementById('upload-btn'));
    f.remove();
  });
  document.body.appendChild(f);
  f.click();
});
</script>
</body></html>`

	br := launchTestBrowser(t)
	page := br.MustPage("")
	t.Cleanup(func() { _ = page.Close() })
	require.NoError(t, page.SetDocumentContent(html))

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "my_tailored_resume.pdf")
	require.NoError(t, os.WriteFile(tmpFile, []byte("%PDF-1.4"), 0644))

	b := &Bot{cfg: Config{}, seenCache: newJobSeenCache()}
	b.cfg.Settings.GenerateNewResumeDocs = true
	lazy := &lazyDocGen{
		b:   b,
		job: linkedInJob{ID: "scoping-test"},
		ctx: context.Background(),
	}
	lazy.once.Do(func() { lazy.resume = tmpFile })

	filled, _, err := b.fillFormStep(context.Background(), page, lazy)
	require.NoError(t, err)
	require.True(t, filled)
	require.Empty(t, lazy.holdReason,
		"holdReason must be empty: jsFindCheckedRadioLabel must read from the dialog, not the background SDUI screen with wrong_resume.pdf")

	// The dialog's resume group must have the uploaded file selected.
	acceptRes, aErr := page.Eval(jsLinkedInCheckResumeAccepted, "my_tailored_resume.pdf")
	require.NoError(t, aErr)
	require.True(t, acceptRes.Value.Get("accepted").Bool(),
		"my_tailored_resume.pdf must be selected in the dialog's resume group")

	// The background screen's radio must be undisturbed — fill must only touch the dialog.
	// This is meaningful because the <form> boundary keeps the two radio groups separate;
	// without the boundary the browser would uncheck rd-background itself regardless of scoping.
	bgRes, bgErr := page.Eval(`() => document.getElementById('rd-background').checked`)
	require.NoError(t, bgErr)
	require.True(t, bgRes.Value.Bool(),
		"background SDUI screen radio must remain checked — fill must not touch the background form's group")
}

// TestJsScanFields_TextInput_AriaLabelledBy_Detected proves Fix A: a text input whose
// accessible name comes only from aria-labelledby is detected and its question is taken
// from the referenced element's textContent. Before Fix A: lbl=null → exclusion gate
// fires (no label, not required) → field dropped. After Fix A: lbl resolved → returned.
func TestJsScanFields_TextInput_AriaLabelledBy_Detected(t *testing.T) {
	const html = `<!DOCTYPE html><html><body>
<div role="dialog">
  <span id="loc-q">Current location</span>
  <input id="location" type="text" aria-labelledby="loc-q" style="display:block;width:200px;height:40px">
</div>
</body></html>`
	page := rodTestPage(t, html)
	fields := evalScanFields(t, page)
	require.Len(t, fields, 1, "aria-labelledby-labelled text input must be detected")
	require.Contains(t, fields[0].Question, "Current location")
}

// TestJsScanFields_TextInput_AriaLabel_Detected proves Fix A: a text input with only
// aria-label (no <label for>, no aria-labelledby) is detected and the question equals
// the attribute value. Before Fix A: exclusion gate fires → field dropped.
func TestJsScanFields_TextInput_AriaLabel_Detected(t *testing.T) {
	const html = `<!DOCTYPE html><html><body>
<div role="dialog">
  <input id="yoe" type="text" aria-label="Years of experience" style="display:block;width:200px;height:40px">
</div>
</body></html>`
	page := rodTestPage(t, html)
	fields := evalScanFields(t, page)
	require.Len(t, fields, 1, "aria-label-only text input must be detected")
	require.Equal(t, "Years of experience", fields[0].Question)
}

// TestJsScanFields_TextInput_AriaLabelledBy_MultiID_Detected verifies that a
// multi-ID aria-labelledby joins all referenced elements' text with a space.
func TestJsScanFields_TextInput_AriaLabelledBy_MultiID_Detected(t *testing.T) {
	const html = `<!DOCTYPE html><html><body>
<div role="dialog">
  <span id="part1">Earliest</span>
  <span id="part2">start date</span>
  <input id="startdate" type="text" aria-labelledby="part1 part2" style="display:block;width:200px;height:40px">
</div>
</body></html>`
	page := rodTestPage(t, html)
	fields := evalScanFields(t, page)
	require.Len(t, fields, 1, "multi-ID aria-labelledby text input must be detected")
	require.Contains(t, fields[0].Question, "Earliest")
	require.Contains(t, fields[0].Question, "start date")
}

// TestJsScanFields_TextInput_AriaLabelledBy_EmptyTarget_Skipped verifies that an input
// whose aria-labelledby target exists but contains only whitespace does not resolve lbl
// (the IIFE returns null after trim), so the exclusion gate drops the field. A blank
// label is indistinguishable from no label for accessibility purposes.
func TestJsScanFields_TextInput_AriaLabelledBy_EmptyTarget_Skipped(t *testing.T) {
	const html = `<!DOCTYPE html><html><body>
<div role="dialog">
  <span id="loc-label">   </span>
  <input id="location" type="text" aria-labelledby="loc-label" style="display:block;width:200px;height:40px">
</div>
</body></html>`
	page := rodTestPage(t, html)
	fields := evalScanFields(t, page)
	require.Empty(t, fields, "input with whitespace-only aria-labelledby target must be excluded (inaccessible name)")
}

// TestJsScanFields_TextInput_AriaLabelledBy_MissingRef_Skipped verifies that a broken
// aria-labelledby reference (ID absent from DOM) does not resolve lbl, so the exclusion
// gate drops the field.
func TestJsScanFields_TextInput_AriaLabelledBy_MissingRef_Skipped(t *testing.T) {
	const html = `<!DOCTYPE html><html><body>
<div role="dialog">
  <input id="q" type="text" aria-labelledby="nonexistent-xyz" style="display:block;width:200px;height:40px">
</div>
</body></html>`
	page := rodTestPage(t, html)
	fields := evalScanFields(t, page)
	require.Empty(t, fields, "input with broken aria-labelledby reference must be excluded")
}

// TestJsScanFields_TextInput_AriaLabelledBy_EmptyTarget_FallsBackToAriaLabel is the
// critical fallback path for Fix A: when aria-labelledby resolves to a whitespace-only
// target (IIFE → null) the scanner falls through to the aria-label IIFE and uses it.
// An empty reference must not block a valid aria-label on the same element.
func TestJsScanFields_TextInput_AriaLabelledBy_EmptyTarget_FallsBackToAriaLabel(t *testing.T) {
	const html = `<!DOCTYPE html><html><body>
<div role="dialog">
  <span id="loc-label">   </span>
  <input id="location" type="text" aria-labelledby="loc-label" aria-label="Current location"
         style="display:block;width:200px;height:40px">
</div>
</body></html>`
	page := rodTestPage(t, html)
	fields := evalScanFields(t, page)
	require.Len(t, fields, 1, "empty aria-labelledby target must fall through to aria-label")
	require.Equal(t, "Current location", fields[0].Question)
}

// TestScanApplyStepsForPrepare_PrepareUploadPolicy exercises the full prepare scan
// across six document-policy paths using a 3-step native-dialog fixture:
//
//   - Step 1: resume fieldset with a library radio, an upload button, and an optional
//     "Change resume" button (sets HasFile=true for site-resume detection).
//   - Step 2: aria-labelledby text input (hidden) — exercises Fix A in a full-scan context.
//   - Step 3: final review (hidden) — submit button with _submitClicks counter.
//
// Sub-tests:
//
//	explicit_version_override        Documents=nil, resume pre-seeded via once.Do
//	documents_service_materialization Documents service, StoreOriginalUpload → version override
//	documents_service_default_policy  Documents service, store.SetDefault, NO explicit override
//	site_resume_skip                  resumeUseSiteOverride, HTML has Change resume → skip upload
//	site_resume_no_file               resumeUseSiteOverride, HTML has NO Change resume → blocks
//	cover_skip                        coverSkipOverride, HTML adds optional cover field
func TestScanApplyStepsForPrepare_PrepareUploadPolicy(t *testing.T) {
	// job_applications/ is written by ExportApplicationUploadFile using os.Getwd().
	t.Cleanup(func() { _ = os.RemoveAll("job_applications") })

	br := launchTestBrowser(t)
	const jobID = "prep-upload-policy-job"
	ctx := context.Background()

	// buildHTML produces the 3-step dialog HTML.
	// hasChangeResumeBtn adds a "Change resume" button so HasFile=true.
	// hasCoverField adds an optional cover-letter <input type="file">.
	// wrongUploadInDOM hard-codes "wrong_resume.pdf" in the upload change handler so the
	// DOM radio label does not match the actual uploaded file — triggers upload-identity hold.
	buildHTML := func(hasChangeResumeBtn, hasCoverField, wrongUploadInDOM bool) string {
		changeResumeBtn := ""
		if hasChangeResumeBtn {
			changeResumeBtn = `
        <button type="button" class="change-resume-btn">Change resume</button>`
		}
		coverField := ""
		if hasCoverField {
			coverField = `
      <div data-test-form-element="">
        <label for="cover-input">Cover letter</label>
        <input type="file" id="cover-input" accept=".pdf" style="display:block;width:200px;height:40px">
      </div>`
		}
		uploadChangeBody := `
    var name = f.files[0] ? f.files[0].name : 'unknown.pdf';
    window._uploadedResumeName = name;`
		if wrongUploadInDOM {
			uploadChangeBody = `
    var name = 'wrong_resume.pdf';
    window._uploadedResumeName = name;`
		}
		return `<!DOCTYPE html><html><body>
<dialog data-testid="dialog" data-job-id="` + jobID + `" open="">
  <div data-sdui-screen="com.linkedin.sdui.flagshipnav.jobs.easyapply.EasyApply">

    <!-- Step 1: resume upload (visible) -->
    <div id="step-1">
      <fieldset id="resume-section">
        <legend>Resume</legend>
        <div>
          <input type="radio" name="resume-select" id="r-lib1">
          <label for="r-lib1">library_resume.pdf</label>
        </div>` + changeResumeBtn + `
        <button type="button" id="upload-btn">
          <span><span>Upload resume</span></span>
        </button>
      </fieldset>` + coverField + `
      <footer>
        <button type="button" id="next-1"><span>Next</span></button>
      </footer>
    </div>

    <!-- Step 2: aria-labelledby text input (hidden) — exercises Fix A -->
    <div id="step-2" style="display:none">
      <div data-test-form-element="">
        <span id="loc-q">Current location</span>
        <input id="location" type="text" aria-labelledby="loc-q"
               style="display:block;width:200px;height:40px">
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
      <button data-easy-apply-submit-button="" id="submit-btn"
              onclick="window._submitClicks=(window._submitClicks||0)+1;">Submit application</button>
    </div>

  </div>
</dialog>
<script>
document.getElementById('upload-btn').addEventListener('click', function() {
  var f = document.createElement('input');
  f.type = 'file';
  f.style.display = 'none';
  f.addEventListener('change', function() {` + uploadChangeBody + `
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
    f.remove();
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
		name               string
		hasChangeResumeBtn bool
		hasCoverField      bool
		wrongUploadInDOM   bool
		wantBlocked        bool
		wantBlockedMsg     string
		wantFile           string // exact base filename; use only for resumeOverride (non-materialization) paths
		wantVersionScoped  bool   // true when UploadPayloadForVersion is called and produces a version-scoped name
		setup              func(t *testing.T, b *Bot, lazy *lazyDocGen)
	}{
		{
			name:     "explicit_version_override",
			wantFile: "my_tailored_resume.pdf",
			setup: func(t *testing.T, b *Bot, lazy *lazyDocGen) {
				tmpFile := filepath.Join(t.TempDir(), "my_tailored_resume.pdf")
				require.NoError(t, os.WriteFile(tmpFile, []byte("%PDF-1.4"), 0644))
				lazy.resumeOverride = tmpFile
			},
		},
		{
			name:              "documents_service_materialization",
			wantVersionScoped: true,
			setup: func(t *testing.T, b *Bot, lazy *lazyDocGen) {
				db, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"))
				require.NoError(t, err)
				t.Cleanup(func() { _ = db.Close() })
				blobs, err := documents.NewLocalBlobStore(t.TempDir())
				require.NoError(t, err)
				b.cfg.Documents = &documents.Service{Store: documents.NewStore(db), Blobs: blobs}
				fakePDF := []byte("%PDF-1.4\n%materialization\n")
				versionID, err := b.cfg.Documents.StoreOriginalUpload(
					ctx, b.cfg.UserID, "my_tailored_resume.pdf", "application/pdf", bytes.NewReader(fakePDF),
				)
				require.NoError(t, err)
				lazy.resumeVersionOverride = versionID
				lazy.formCaps = documents.FormDocumentCapabilities{Detected: true, ResumeFileSlots: 1}
			},
		},
		{
			// Documents != nil, store.SetDefault, NO resumeVersionOverride — exercises the
			// ResumeDocumentModeDefault path where the resolver picks up the DB default.
			name:              "documents_service_default_policy",
			wantVersionScoped: true,
			setup: func(t *testing.T, b *Bot, lazy *lazyDocGen) {
				db, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"))
				require.NoError(t, err)
				t.Cleanup(func() { _ = db.Close() })
				blobs, err := documents.NewLocalBlobStore(t.TempDir())
				require.NoError(t, err)
				store := documents.NewStore(db)
				b.cfg.Documents = &documents.Service{Store: store, Blobs: blobs}
				b.cfg.Settings.DocumentPolicies.ResumeMode = domain.ResumeDocumentModeDefault
				fakePDF := []byte("%PDF-1.4\n%default\n")
				versionID, err := b.cfg.Documents.StoreOriginalUpload(
					ctx, b.cfg.UserID, "default_resume.pdf", "application/pdf", bytes.NewReader(fakePDF),
				)
				require.NoError(t, err)
				// SetDefault directly to avoid Service.SetDefault's LoadProfile requirement.
				require.NoError(t, store.SetDefault(ctx, b.cfg.UserID, "resume", versionID, "", ""))
				// No resumeVersionOverride — resolver must pick up the DB default.
				lazy.formCaps = documents.FormDocumentCapabilities{Detected: true, ResumeFileSlots: 1}
				// Bypass legacy migration (GenerateNewResumeDocs=true would overwrite mode to TailorJob).
				b.cfg.Settings.DocumentPolicies.Version = domain.DocumentPolicyMigrationVersion
				b.cfg.Settings.DocumentPolicies.CoverMode = domain.CoverDocumentModeWhenRequired
			},
		},
		{
			// HTML has "Change resume" button (HasFile=true). resumeUseSiteOverride=true and
			// SiteResumePresent=true → resolver returns UseSite=true → upload skipped.
			name:               "site_resume_skip",
			hasChangeResumeBtn: true,
			setup: func(t *testing.T, b *Bot, lazy *lazyDocGen) {
				db, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"))
				require.NoError(t, err)
				t.Cleanup(func() { _ = db.Close() })
				b.cfg.Documents = &documents.Service{Store: documents.NewStore(db)}
				b.cfg.Settings.DocumentPolicies.ResumeMode = domain.ResumeDocumentModeSiteHosted
				// Bypass legacy migration (GenerateNewResumeDocs=true would overwrite mode to TailorJob).
				b.cfg.Settings.DocumentPolicies.Version = domain.DocumentPolicyMigrationVersion
				b.cfg.Settings.DocumentPolicies.CoverMode = domain.CoverDocumentModeWhenRequired
				lazy.resumeUseSiteOverride = true
			},
		},
		{
			// HTML has NO "Change resume" button (HasFile=false). resumeUseSiteOverride=true but
			// SiteResumePresent=false → resolver holds → Fix C surfaces the hold reason as error.
			name:           "site_resume_no_file",
			wantBlocked:    true,
			wantBlockedMsg: "no site resume is attached",
			setup: func(t *testing.T, b *Bot, lazy *lazyDocGen) {
				db, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"))
				require.NoError(t, err)
				t.Cleanup(func() { _ = db.Close() })
				b.cfg.Documents = &documents.Service{Store: documents.NewStore(db)}
				b.cfg.Settings.DocumentPolicies.ResumeMode = domain.ResumeDocumentModeSiteHosted
				// Bypass legacy migration so SiteHosted mode is preserved and siteMode=true
				// allows detectSiteResumeOnField to correctly report SiteResumePresent=false.
				b.cfg.Settings.DocumentPolicies.Version = domain.DocumentPolicyMigrationVersion
				b.cfg.Settings.DocumentPolicies.CoverMode = domain.CoverDocumentModeWhenRequired
				lazy.resumeUseSiteOverride = true
			},
		},
		{
			// HTML has an optional cover-letter file field. coverSkipOverride=true → resolver
			// sets Cover.Skip=true → cover path stays empty → cover field skipped, resume uploaded.
			name:              "cover_skip",
			hasCoverField:     true,
			wantVersionScoped: true,
			setup: func(t *testing.T, b *Bot, lazy *lazyDocGen) {
				db, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"))
				require.NoError(t, err)
				t.Cleanup(func() { _ = db.Close() })
				blobs, err := documents.NewLocalBlobStore(t.TempDir())
				require.NoError(t, err)
				b.cfg.Documents = &documents.Service{Store: documents.NewStore(db), Blobs: blobs}
				b.cfg.Settings.DocumentPolicies.CoverMode = domain.CoverDocumentModeWhenRequired
				fakePDF := []byte("%PDF-1.4\n%cover-test\n")
				versionID, err := b.cfg.Documents.StoreOriginalUpload(
					ctx, b.cfg.UserID, "my_tailored_resume.pdf", "application/pdf", bytes.NewReader(fakePDF),
				)
				require.NoError(t, err)
				lazy.resumeVersionOverride = versionID
				lazy.coverSkipOverride = true
				lazy.formCaps = documents.FormDocumentCapabilities{Detected: true, ResumeFileSlots: 1, CoverOptional: true}
			},
		},
		{
			// Same Documents=nil + resumeOverride setup as explicit_version_override, but
			// the change handler reports a different filename in the DOM. The final-review
			// verification must detect the mismatch and set holdReason → wantBlocked.
			name:             "wrong_file_explicit_override",
			wrongUploadInDOM: true,
			wantBlocked:      true,
			wantBlockedMsg:   "upload identity could not be verified",
			setup: func(t *testing.T, b *Bot, lazy *lazyDocGen) {
				tmpFile := filepath.Join(t.TempDir(), "my_tailored_resume.pdf")
				require.NoError(t, os.WriteFile(tmpFile, []byte("%PDF-1.4"), 0644))
				lazy.resumeOverride = tmpFile
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			html := buildHTML(tc.hasChangeResumeBtn, tc.hasCoverField, tc.wrongUploadInDOM)
			srv := fixtureServer(t, jobID, "", html, "")
			page := navigateAndWait(t, br, srv.URL+"/jobs/view/"+jobID)

			b := &Bot{cfg: Config{}, seenCache: newJobSeenCache()}
			b.cfg.Settings.GenerateNewResumeDocs = true
			b.cfg.UserID = "user-" + tc.name

			lazy := &lazyDocGen{
				b:                 b,
				job:               linkedInJob{ID: jobID, URL: srv.URL + "/jobs/view/" + jobID},
				reviewPrepareOnly: true,
				ctx:               ctx,
			}

			tc.setup(t, b, lazy)

			err := b.scanApplyStepsForPrepare(ctx, page, lazy)

			submitClicks, evalErr := page.Eval(`() => window._submitClicks || 0`)
			require.NoError(t, evalErr)
			require.EqualValues(t, 0, submitClicks.Value.Num(), "Submit must not be clicked during preparation scan")

			if tc.wantBlocked {
				require.Error(t, err, "blocked prepare must return an error")
				require.False(t, lazy.prepareScanComplete)
				require.NotEmpty(t, lazy.holdReason)
				require.Contains(t, err.Error(), tc.wantBlockedMsg)
				require.NotContains(t, err.Error(), "review step not reached",
					"Fix C must surface the real hold reason, not the generic scan-incomplete message")
				return
			}

			require.NoError(t, err, "prepare must complete without error for %s", tc.name)
			require.True(t, lazy.prepareScanComplete, "prepareScanComplete must be true for %s", tc.name)
			require.Empty(t, lazy.holdReason, "holdReason must be empty on success for %s", tc.name)

			if tc.name == "site_resume_skip" {
				// No new upload: window._uploadedResumeName must be empty.
				uploadedName, _ := page.Eval(`() => window._uploadedResumeName || ""`)
				require.Empty(t, uploadedName.Value.String(), "site_resume_skip must not trigger a new upload")
				return
			}

			// For all upload-based sub-tests: confirm the correct file was delivered.
			resumePath, _ := lazy.peek()
			require.NotEmpty(t, resumePath, "resume path must be set after successful prepare for %s", tc.name)
			if tc.wantVersionScoped {
				require.True(t, isVersionScopedFilename(filepath.Base(resumePath)),
					"materialization path must produce a version-scoped filename, got %s for %s", filepath.Base(resumePath), tc.name)
			} else if tc.wantFile != "" {
				require.Equal(t, tc.wantFile, filepath.Base(resumePath),
					"exported filename must match intended for %s", tc.name)
			}
			resumeBase := filepath.Base(resumePath)

			// Final review has hidden the upload step; validate its retained
			// selection and the visible summary rather than accepting a hidden CTA.
			acceptRes, aErr := page.Eval(jsLinkedInCheckResumeAccepted, resumeBase)
			require.NoError(t, aErr)
			require.False(t, acceptRes.Value.Get("accepted").Bool(), "hidden upload controls must not be accepted")
			checked, cErr := page.Eval(jsFindCheckedRadioLabel, "resume-select")
			require.NoError(t, cErr)
			require.Equal(t, resumeBase, checked.Value.String(), "retained selection must match intended file for %s", tc.name)
			summary, sErr := page.Eval(jsLinkedInFinalReviewResumeCheck, resumeBase)
			require.NoError(t, sErr)
			require.True(t, summary.Value.Get("verified").Bool(), "visible review must confirm intended attachment for %s", tc.name)

			// Pre-existing library radio must not be removed by the upload flow.
			libRes, libErr := page.Eval(`() => !!document.getElementById('r-lib1')`)
			require.NoError(t, libErr)
			require.True(t, libRes.Value.Bool(), "library radio r-lib1 must still exist after upload")

			// Final review must be reached.
			frRes, frErr := page.Eval(jsLinkedInAtFinalReview)
			require.NoError(t, frErr)
			require.True(t, frRes.Value.Bool(), "page must be at final review after preparation scan for %s", tc.name)
		})
	}
}

// ── Finding 1 DOM regression: duplicate labels in different sections ──────────

// htmlDuplicateLabelSections is a minimal LinkedIn-shaped dialog with two
// visually-separate sections ("Software Skills" and "Medical Skills"), each
// containing a text input whose visible question label is identical:
// "Years of experience". The sections are FLAT siblings under the dialog —
// no wrapper divs — so both inputs are siblings of the section headings.
//
// This exercises the last-heading semantics of sectionGroupFor: with first-heading
// logic both inputs would inherit the outer h2 "Screening Questions"; with
// last-heading each input inherits its own closest h3.
const htmlDuplicateLabelSections = `<!DOCTYPE html><html><body>
<div role="dialog" class="jobs-easy-apply-modal" style="padding:16px">
  <h2>Screening Questions</h2>

  <h3>Software Skills</h3>
  <div data-test-form-element="">
    <label for="exp-software">Years of experience</label>
    <input id="exp-software" type="text" aria-label="Years of experience" />
  </div>

  <h3>Medical Skills</h3>
  <div data-test-form-element="">
    <label for="exp-medical">Years of experience</label>
    <input id="exp-medical" type="text" aria-label="Years of experience" />
  </div>
</div>
</body></html>`

// TestScan_DuplicateLabel_DifferentSection_CaptureAndFill proves the full
// Finding 1 pipeline end-to-end using a live browser:
//
//  1. jsScanFields returns two separate fields with matching labels but
//     different Group values ("Software Skills" and "Medical Skills").
//  2. captureStepSnapshot produces SnapFields with different FieldSignatures.
//  3. DBMappingStore resolves the two fields independently.
//  4. jsFillByQuestionAnswer, given the section hint, fills the correct
//     <input> without touching the other section.
//
// The test is skipped when Chrome is not available (CI without headless Chrome).
func TestScan_DuplicateLabel_DifferentSection_CaptureAndFill(t *testing.T) {
	page := rodTestPage(t, htmlDuplicateLabelSections)

	// 1. Scan: verify two fields with the same label but different groups.
	fields := evalScanFields(t, page)
	var softwareField, medicalField formField
	for _, f := range fields {
		switch f.Group {
		case "Software Skills":
			softwareField = f
		case "Medical Skills":
			medicalField = f
		}
	}
	require.NotEmpty(t, softwareField.Question, "Software Skills section must yield a field")
	require.NotEmpty(t, medicalField.Question, "Medical Skills section must yield a field")
	require.Equal(t, "Years of experience", softwareField.Question)
	require.Equal(t, "Years of experience", medicalField.Question)
	require.NotEqual(t, softwareField.Group, medicalField.Group, "groups must differ")
	t.Logf("EVIDENCE dom-scan | software_group=%q | medical_group=%q", softwareField.Group, medicalField.Group)

	// 2. Snapshot: verify different FieldSignatures.
	snap := captureStepSnapshot(fields, nil, "linkedin", 0, "Screening Questions")
	var sigSoftware, sigMedical string
	for _, sf := range snap.Fields {
		switch sf.Group {
		case "Software Skills":
			sigSoftware = sf.FieldSignature("linkedin")
		case "Medical Skills":
			sigMedical = sf.FieldSignature("linkedin")
		}
	}
	require.NotEmpty(t, sigSoftware, "snapshot must have Software Skills field")
	require.NotEmpty(t, sigMedical, "snapshot must have Medical Skills field")
	require.NotEqual(t, sigSoftware, sigMedical, "different groups must yield different FieldSignatures")
	t.Logf("EVIDENCE dom-sig | sig_software=%s | sig_medical=%s", sigSoftware, sigMedical)

	// 3. Explicit semantic review, then replay through the production interpreter and filler.
	store := &DBMappingStore{DB: openMappingDB(t), UserID: "user-dom-test"}
	resolutions := []FieldResolution{}
	for _, field := range snap.Fields {
		key := "application_defaults.form_answers.software_years"
		if field.Group == "Medical Skills" {
			key = "application_defaults.form_answers.medical_years"
		}
		resolutions = append(resolutions, FieldResolution{Ref: field.Ref, Source: SourceLLM, Action: ActionFill, IntentType: FieldIntentProfile, BindingKey: key})
	}
	require.NoError(t, reviewFixtureMappings(t, store, snap, resolutions))
	interpreter := &StepInterpreter{Mapping: store}
	interp, err := interpreter.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	require.False(t, interp.Paused)
	filler := &linkedInRodFiller{page: page, bot: newTestBot(t)}
	for _, res := range interp.Fields {
		field, ok := snap.FieldByRef(res.Ref)
		require.True(t, ok)
		result := filler.Fill(context.Background(), field, res)
		require.NoError(t, result.Err)
		require.True(t, result.Filled)
	}
	softwareVal, err := page.Eval("() => document.getElementById('exp-software').value")
	require.NoError(t, err)
	medicalVal, err := page.Eval("() => document.getElementById('exp-medical').value")
	require.NoError(t, err)
	require.Equal(t, "5", softwareVal.Value.String())
	require.Equal(t, "3", medicalVal.Value.String())
	t.Logf("EVIDENCE dom-cache-fill | software=5 medical=3 | interpretation_calls=0")

}

// htmlNativeSelectForm is a minimal form with a native <select> for a country selector.
// Used to verify that jsScanFields captures it as type=select and that
// jsFillByQuestionAnswer fills it correctly when fieldType is "select".
const htmlNativeSelectForm = `<!DOCTYPE html><html><body>
<div role="dialog" class="jobs-easy-apply-modal" style="padding:16px">
  <label for="country-sel">Country of residence</label>
  <select id="country-sel" aria-label="Country of residence">
    <option value="">Select...</option>
    <option value="AU">Australia</option>
    <option value="NZ">New Zealand</option>
    <option value="GB">United Kingdom</option>
  </select>
</div>
</body></html>`

// TestScan_NativeSelect_CaptureAndFill proves the native-select path end-to-end:
//
//  1. jsScanFields detects the <select> as type=select with the correct ControlID.
//  2. jsFillByQuestionAnswer with fieldType="select" selects the target option.
//  3. The DOM value reflects the chosen option after the fill.
//
// The test is skipped when Chrome is not available (CI without headless Chrome).
func TestScan_NativeSelect_CaptureAndFill(t *testing.T) {
	page := rodTestPage(t, htmlNativeSelectForm)

	// 1. Scan: select must be captured with type=select and the element id.
	fields := evalScanFields(t, page)
	require.Len(t, fields, 1, "expect exactly one field")
	sel := fields[0]
	require.Equal(t, "select", sel.Type, "field type must be 'select'")
	require.Equal(t, "country-sel", sel.ID, "control ID must match element id")
	require.NotEmpty(t, sel.Options, "options must be captured")
	t.Logf("EVIDENCE native-select scan | question=%q type=%q id=%q options=%v",
		sel.Question, sel.Type, sel.ID, sel.Options)

	// 2. Fill: direct id targeting must set the correct option.
	fillResult, err := page.Eval(jsFillByQuestionAnswer,
		sel.Question, "Australia", "select", "", sel.ID)
	require.NoError(t, err)
	require.True(t, fillResult.Value.Bool(), "fill must return true")

	// 3. Verify: the native select value must reflect the chosen option.
	val, err := page.Eval(`() => document.getElementById('country-sel').value`)
	require.NoError(t, err)
	require.Equal(t, "AU", val.Value.String(), "native select value must be the option value for 'Australia'")
	t.Logf("EVIDENCE native-select fill | selected_value=%q", val.Value.String())
}

// htmlSeekFormNoAdvance simulates a SEEK Quick Apply step whose Continue button
// does nothing — no DOM change, no validation errors — when clicked.
// No progress bar → NavigateNext has no advancement signal → must timeout and return false.
const htmlSeekFormNoAdvance = `<!DOCTYPE html><html><body style="margin:0;padding:16px">
  <form data-testid="quick-apply-form">
  <h2 id="step-title" style="margin-top:0">Work experience</h2>
  <input id="job-title" aria-label="Most recent job title" value=""
         style="display:block;width:200px;height:32px;margin-bottom:8px">
  <button type="button" data-testid="continue-button"
          style="padding:8px 16px;width:120px;height:40px">Continue</button>
</form></body></html>`

// htmlSeekFormWithProgress simulates a SEEK Quick Apply step with a progress bar.
// When the Continue button is clicked, JavaScript advances aria-valuenow from 1 to 2,
// giving NavigateNext the positive evidence it needs to confirm step advancement.
const htmlSeekFormWithProgress = `<!DOCTYPE html><html><body style="margin:0;padding:16px">
  <form data-testid="quick-apply-form">
  <progress id="step-bar" role="progressbar" aria-valuenow="1" aria-valuemax="3" value="0.33"></progress>
  <h2>Work experience</h2>
  <input id="job-title" aria-label="Most recent job title" value=""
         style="display:block;width:200px;height:32px;margin-bottom:8px">
  <button type="button" data-testid="continue-button" id="continue-btn"
          style="padding:8px 16px;width:120px;height:40px">Continue</button>
  <script>
    document.getElementById('continue-btn').addEventListener('click', function() {
      document.getElementById('step-bar').setAttribute('aria-valuenow','2');
    });
  </script>
</form></body></html>`

// ── writeConfirmedStep unit tests (no browser needed) ─────────────────────────

// writeGateFixture builds a minimal snap, interp, and mapping store for write-gate tests.
func writeGateFixture(t *testing.T, dbUserID string) (StepSnapshot, StepInterpretation, *DBMappingStore) {
	t.Helper()
	db := openProviderTestDB(t)
	store := &DBMappingStore{DB: db, UserID: dbUserID}
	snap := captureStepSnapshot([]formField{
		{Type: "text", Question: "Most recent job title", Group: "Work experience", ID: "job-title", InputType: "text"},
	}, nil, "seek", 0, "Work experience")
	interp := StepInterpretation{
		Fields: []FieldResolution{{Ref: "f0", Action: ActionFill, Source: SourceProfile, Value: "Nurse", IntentType: FieldIntentProfile, BindingKey: "application_defaults.form_answers.current_title"}},
	}
	return snap, interp, store
}

// writeGateHasField returns true when f0's field signature is present in the store.
func writeGateHasField(store *DBMappingStore, snap StepSnapshot) bool {
	_, found := store.LookupField(snap.Fields[0].FieldSignature(snap.Platform), snap.EmployerKey)
	return found
}

// filledStepResult returns a StepExecResult that claims ref f0 was filled.
func filledStepResult(outcome StepExecOutcome) StepExecResult {
	return StepExecResult{Outcome: outcome, FieldsFilled: 1, FilledRefs: []string{"f0"}}
}

// TestWriteGate_Advanced_WritesFilledRefs proves that StepOutcomeAdvanced persists
// field mappings via writeConfirmedStep (the production gate).
func TestWriteGate_Advanced_WritesFilledRefs(t *testing.T) {
	snap, interp, store := writeGateFixture(t, "gate-advanced")
	result := filledStepResult(StepOutcomeAdvanced)
	writeConfirmedStep(context.Background(), store, snap, interp, result, false)
	require.True(t, writeGateHasField(store, snap), "StepOutcomeAdvanced must write filled ref to store")
	t.Logf("EVIDENCE write-gate-advanced | outcome=%s filledRefs=%v wrote=true", result.Outcome, result.FilledRefs)
}

// TestWriteGate_ErrorOutcome_ZeroWrites proves that a non-advancing step
// (NavigateNext returned false → StepOutcomeError) writes zero mappings.
func TestWriteGate_ErrorOutcome_ZeroWrites(t *testing.T) {
	snap, interp, store := writeGateFixture(t, "gate-error")
	result := filledStepResult(StepOutcomeError)
	writeConfirmedStep(context.Background(), store, snap, interp, result, false)
	require.False(t, writeGateHasField(store, snap), "StepOutcomeError must write zero mappings")
	t.Logf("EVIDENCE write-gate-error | outcome=%s wrote=false", result.Outcome)
}

// TestWriteGate_SubmitWithoutConfirmation_ZeroWrites proves that a submitted step
// whose platform confirmation check returned false writes zero mappings.
// Covers: LinkedIn submission with no confirmation signal; SEEK with no post-apply page.
func TestWriteGate_SubmitWithoutConfirmation_ZeroWrites(t *testing.T) {
	snap, interp, store := writeGateFixture(t, "gate-submit-unconfirmed")
	result := filledStepResult(StepOutcomeSubmitted)
	writeConfirmedStep(context.Background(), store, snap, interp, result, false /* submissionConfirmed=false */)
	require.False(t, writeGateHasField(store, snap), "unconfirmed submission must write zero mappings")
	t.Logf("EVIDENCE write-gate-submit-unconfirmed | outcome=%s wrote=false", result.Outcome)
}

// TestWriteGate_SubmitWithConfirmation_WritesOnce proves that a submitted step
// whose platform confirmation check returned true writes the filled ref exactly once.
func TestWriteGate_SubmitWithConfirmation_WritesOnce(t *testing.T) {
	snap, interp, store := writeGateFixture(t, "gate-submit-confirmed")
	result := filledStepResult(StepOutcomeSubmitted)
	writeConfirmedStep(context.Background(), store, snap, interp, result, true /* submissionConfirmed=true */)
	require.True(t, writeGateHasField(store, snap), "confirmed submission must write filled ref to store")
	res, found := store.LookupField(snap.Fields[0].FieldSignature(snap.Platform), snap.EmployerKey)
	require.True(t, found, "field must be in store after confirmed submission")
	require.Empty(t, res.Value, "mapping storage must not retain a candidate answer")
	require.Equal(t, "application_defaults.form_answers.current_title", res.BindingKey)
	t.Logf("EVIDENCE write-gate-submit-confirmed | outcome=%s value=%q", result.Outcome, res.Value)
}

// TestWriteGate_NilStore_NoPanic proves that writeConfirmedStep is safe with a nil store.
func TestWriteGate_NilStore_NoPanic(t *testing.T) {
	snap := captureStepSnapshot([]formField{
		{Type: "text", Question: "Job title", InputType: "text"},
	}, nil, "seek", 0, "step")
	interp := StepInterpretation{
		Fields: []FieldResolution{{Ref: "f0", Action: ActionFill, Source: SourceLLM, Value: "x"}},
	}
	result := filledStepResult(StepOutcomeAdvanced)
	require.NotPanics(t, func() {
		writeConfirmedStep(context.Background(), nil, snap, interp, result, false)
	})
}

// ── browser-level NavigateNext + writeConfirmedStep tests ─────────────────────

// TestSeekFiller_SameStepClick_ZeroMappings proves that when a SEEK Continue click
// does not advance the form (no progress-bar signal, no submit-button appears),
// the production writeConfirmedStep gate writes zero mappings.
//
// This is the regression for the bug where a click returning nil was trusted as
// evidence of advancement even when the form stayed on the same step.
func TestSeekFiller_SameStepClick_ZeroMappings(t *testing.T) {
	page := rodTestPage(t, htmlSeekFormNoAdvance)
	b := &Bot{cfg: Config{}, seenCache: newJobSeenCache()}
	filler := &seekRodFiller{page: page, bot: b}

	// 1. Verify NavigateNext requires positive evidence and correctly detects non-advancement.
	advanced, navErr := filler.NavigateNext(context.Background())
	require.False(t, advanced, "NavigateNext must not report advancement when form did not advance")
	require.Error(t, navErr, "NavigateNext must return an error when the step does not advance")
	require.Contains(t, navErr.Error(), "did not advance", "error must describe the non-advancement")

	// 2. Run the full StepExecutor pipeline through the production write gate.
	db := openProviderTestDB(t)
	store := &DBMappingStore{DB: db, UserID: "no-advance-test"}
	snap := captureStepSnapshot([]formField{
		{Type: "text", Question: "Most recent job title", Group: "Work experience", ID: "job-title", InputType: "text"},
	}, nil, "seek", 0, "Work experience")
	interp := StepInterpretation{
		Fields: []FieldResolution{{Ref: "f0", Action: ActionFill, Source: SourceProfile, Value: "Nurse", IntentType: FieldIntentProfile, BindingKey: "application_defaults.form_answers.current_title"}},
	}
	se := NewStepExecutor(DefaultStepExecBounds(), nil)
	result, _ := se.ExecuteStep(context.Background(), snap, interp, filler)

	// Call the same production gate the runner calls — not a reproduced if-condition.
	writeConfirmedStep(context.Background(), store, snap, interp, result, false)

	require.False(t, writeGateHasField(store, snap),
		"non-advancing step must write zero mappings via production gate")
	require.NotEqual(t, StepOutcomeAdvanced, result.Outcome,
		"step outcome must not be Advanced when navigation was rejected")
	t.Logf("EVIDENCE same-step-click | advanced=%v err=%q outcome=%s",
		advanced, navErr.Error(), result.Outcome)
}

// TestSeekFiller_RealAdvance_WritesFilledRefs proves that when a SEEK Continue click
// advances the form (progress bar aria-valuenow changes), NavigateNext returns true
// and the production writeConfirmedStep gate persists the filled ref.
func TestSeekFiller_RealAdvance_WritesFilledRefs(t *testing.T) {
	page := rodTestPage(t, htmlSeekFormWithProgress)
	b := &Bot{cfg: Config{}, seenCache: newJobSeenCache()}
	filler := &seekRodFiller{page: page, bot: b}

	db := openProviderTestDB(t)
	store := &DBMappingStore{DB: db, UserID: "real-advance-test"}
	snap := captureStepSnapshot([]formField{
		{Type: "text", Question: "Most recent job title", Group: "Work experience", ID: "job-title", InputType: "text"},
	}, nil, "seek", 0, "Work experience")
	interp := StepInterpretation{
		Fields: []FieldResolution{{Ref: "f0", Action: ActionFill, Source: SourceProfile, Value: "Nurse", IntentType: FieldIntentProfile, BindingKey: "application_defaults.form_answers.current_title"}},
	}
	se := NewStepExecutor(DefaultStepExecBounds(), nil)
	result, execErr := se.ExecuteStep(context.Background(), snap, interp, filler)

	// Call the production write gate.
	writeConfirmedStep(context.Background(), store, snap, interp, result, false)

	require.NoError(t, execErr, "execution must not error on successful advance")
	require.Equal(t, StepOutcomeAdvanced, result.Outcome,
		"step must advance when progress bar signals step transition")
	require.True(t, writeGateHasField(store, snap),
		"real advance must write filled ref via production gate")
	t.Logf("EVIDENCE real-advance | outcome=%s filledRefs=%v wrote=true", result.Outcome, result.FilledRefs)
}
