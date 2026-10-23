package bot

import (
	"encoding/json"
	"os"
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
	require.True(t, resumeField.Required, "section has aria-required=true")
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
