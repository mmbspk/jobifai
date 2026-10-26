package bot

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// nativeDialogWithResume builds a native-dialog page HTML with a resume section.
// If checkedFilename is non-empty, that radio entry is pre-checked (simulating a
// completed upload). If uploadBtnPresent is false, no Upload button is included.
func nativeDialogWithResume(checkedFilename string, uploadBtnPresent bool) string {
	radioChecked := ""
	if checkedFilename != "" {
		radioChecked = `<div>
          <input type="radio" name="resume-select" id="r-lib1" value="lib1" checked>
          <label for="r-lib1">` + checkedFilename + `</label>
        </div>`
	}
	uploadBtn := ""
	if uploadBtnPresent {
		uploadBtn = `<button type="button"><span><span>Upload resume</span></span></button>`
	}
	return `<!DOCTYPE html><html><body>
<dialog data-testid="dialog" open="">
  <div data-sdui-screen="com.linkedin.sdui.flagshipnav.jobs.easyapply.EasyApply">
    <fieldset id="resume-section">
      <legend>Resume <span aria-hidden="true">*</span></legend>
      ` + radioChecked + `
      ` + uploadBtn + `
    </fieldset>
    <footer><button type="button"><span>Next</span></button></footer>
  </div>
</dialog>
</body></html>`
}

// TestLinkedInClickUploadResume_ContainerNotFound verifies that jsLinkedInClickUploadResume
// returns ok=false with a "container not found" reason when no apply dialog is present.
func TestLinkedInClickUploadResume_ContainerNotFound(t *testing.T) {
	page := rodTestPage(t, `<!DOCTYPE html><html><body><p>No dialog here</p></body></html>`)

	res, err := page.Eval(jsLinkedInClickUploadResume)
	require.NoError(t, err)
	require.False(t, res.Value.Get("ok").Bool(), "must return ok=false when container is absent")
	require.Contains(t, res.Value.Get("reason").String(), "container not found")
}

// TestLinkedInClickUploadResume_ButtonNotFound verifies ok=false when the dialog exists
// but contains no "Upload resume" button.
func TestLinkedInClickUploadResume_ButtonNotFound(t *testing.T) {
	html := `<!DOCTYPE html><html><body>
<dialog data-testid="dialog" open="">
  <div data-sdui-screen="com.linkedin.sdui.flagshipnav.jobs.easyapply.EasyApply">
    <div id="resume-section">
      <label>Resume <span aria-hidden="true">*</span></label>
    </div>
  </div>
</dialog>
</body></html>`
	page := rodTestPage(t, html)

	res, err := page.Eval(jsLinkedInClickUploadResume)
	require.NoError(t, err)
	require.False(t, res.Value.Get("ok").Bool(), "must return ok=false when no upload button exists")
	require.Contains(t, res.Value.Get("reason").String(), "not found")
}

// TestLinkedInCheckResumeAccepted_RadioChecked_Match verifies accepted=true and
// filenameVisible=true when the checked radio label contains the expected filename.
func TestLinkedInCheckResumeAccepted_RadioChecked_Match(t *testing.T) {
	page := rodTestPage(t, nativeDialogWithResume("my_resume_2026.pdf", true))

	res, err := page.Eval(jsLinkedInCheckResumeAccepted, "my_resume_2026.pdf")
	require.NoError(t, err)
	require.True(t, res.Value.Get("accepted").Bool(), "checked radio must count as accepted")
	require.False(t, res.Value.Get("error").Bool())
	require.True(t, res.Value.Get("filenameVisible").Bool(), "filename must be visible in label")
}

// TestLinkedInCheckResumeAccepted_RadioChecked_Mismatch verifies accepted=true but
// filenameVisible=false when the checked radio label does not contain the expected filename.
func TestLinkedInCheckResumeAccepted_RadioChecked_Mismatch(t *testing.T) {
	page := rodTestPage(t, nativeDialogWithResume("other_resume.pdf", true))

	res, err := page.Eval(jsLinkedInCheckResumeAccepted, "my_resume_2026.pdf")
	require.NoError(t, err)
	require.True(t, res.Value.Get("accepted").Bool(), "checked radio must count as accepted")
	require.False(t, res.Value.Get("error").Bool())
	require.False(t, res.Value.Get("filenameVisible").Bool(), "filename must not match when label differs")
}

// TestLinkedInCheckResumeAccepted_NoRadioChecked verifies accepted=false when radio
// entries are present but none is checked.
func TestLinkedInCheckResumeAccepted_NoRadioChecked(t *testing.T) {
	page := rodTestPage(t, nativeDialogWithResume("", true))

	res, err := page.Eval(jsLinkedInCheckResumeAccepted, "my_resume_2026.pdf")
	require.NoError(t, err)
	require.False(t, res.Value.Get("accepted").Bool(), "no checked radio means not yet accepted")
	require.False(t, res.Value.Get("error").Bool())
}

// TestLinkedInCheckResumeAccepted_ErrorIndicator verifies error=true when an
// aria-invalid indicator is present.
func TestLinkedInCheckResumeAccepted_ErrorIndicator(t *testing.T) {
	html := `<!DOCTYPE html><html><body>
<dialog data-testid="dialog" open="">
  <div data-sdui-screen="com.linkedin.sdui.flagshipnav.jobs.easyapply.EasyApply">
    <div id="resume-section">
      <label>Resume <span aria-hidden="true">*</span></label>
      <input aria-invalid="true" style="display:block;width:200px;height:40px">
    </div>
  </div>
</dialog>
</body></html>`
	page := rodTestPage(t, html)

	res, err := page.Eval(jsLinkedInCheckResumeAccepted, "my_resume_2026.pdf")
	require.NoError(t, err)
	require.True(t, res.Value.Get("error").Bool(), "must detect error indicator")
	require.False(t, res.Value.Get("accepted").Bool())
}

// TestLinkedInCheckResumeAccepted_ContainerNotFound verifies accepted=false when
// no apply container is present on the page.
func TestLinkedInCheckResumeAccepted_ContainerNotFound(t *testing.T) {
	page := rodTestPage(t, `<!DOCTYPE html><html><body><p>No dialog here</p></body></html>`)

	res, err := page.Eval(jsLinkedInCheckResumeAccepted, "my_resume_2026.pdf")
	require.NoError(t, err)
	require.False(t, res.Value.Get("accepted").Bool())
	require.False(t, res.Value.Get("error").Bool())
}

// TestLinkedInWaitForResumeAccepted_IdentityUnverifiable verifies that when the
// checked radio label does not match the expected filename, errUploadIdentityUnverifiable
// is returned — triggering a HOLD rather than a hard failure.
func TestLinkedInWaitForResumeAccepted_IdentityUnverifiable(t *testing.T) {
	// Page has "other_resume.pdf" checked; we expect "my_resume_2026.pdf".
	page := rodTestPage(t, nativeDialogWithResume("other_resume.pdf", true))

	err := linkedInWaitForResumeAccepted(context.Background(), page, "/uploads/my_resume_2026.pdf")
	require.True(t, errors.Is(err, errUploadIdentityUnverifiable),
		"mismatched filename must return errUploadIdentityUnverifiable, got: %v", err)
}

// TestLinkedInWaitForResumeAccepted_AcceptsCorrectFile verifies that when the checked
// radio label matches the expected filename, nil is returned.
func TestLinkedInWaitForResumeAccepted_AcceptsCorrectFile(t *testing.T) {
	// Page has "my_resume_2026.pdf" checked; same filename is expected.
	page := rodTestPage(t, nativeDialogWithResume("my_resume_2026.pdf", true))

	err := linkedInWaitForResumeAccepted(context.Background(), page, "/uploads/my_resume_2026.pdf")
	require.NoError(t, err, "matching filename must be accepted without error")
}

// TestFormUploadViaButton_ContextCancelled verifies context cancellation unblocks
// formUploadViaButton before the file chooser fires.
func TestFormUploadViaButton_ContextCancelled(t *testing.T) {
	// Upload button triggers file chooser via setTimeout at t=500ms.
	// Context times out at 200ms — must return DeadlineExceeded before chooser fires.
	const html = `<!DOCTYPE html><html><body>
<dialog data-testid="dialog" open="">
  <div data-sdui-screen="com.linkedin.sdui.flagshipnav.jobs.easyapply.EasyApply">
    <fieldset id="resume-section">
      <legend>Resume <span aria-hidden="true">*</span></legend>
      <button type="button" id="upload-btn" onclick="setTimeout(function(){var f=document.createElement('input');f.type='file';f.id='r-chooser';document.body.appendChild(f);f.click();},500)">
        <span><span>Upload resume</span></span>
      </button>
    </fieldset>
    <footer><button type="button"><span>Next</span></button></footer>
  </div>
</dialog>
</body></html>`

	br := launchTestBrowser(t)
	page := br.MustPage("")
	t.Cleanup(func() { _ = page.Close() })
	require.NoError(t, page.SetDocumentContent(html))

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "resume.pdf")
	require.NoError(t, os.WriteFile(tmpFile, []byte("%PDF-1.4"), 0644))

	b := &Bot{cfg: Config{}, seenCache: newJobSeenCache()}
	err := b.formUploadViaButton(ctx, page, tmpFile)
	require.True(t, errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled),
		"must return context error when cancelled before chooser fires, got: %v", err)
}

// TestFormUploadViaButton_TrustedClick_DeliversFile is the end-to-end test that calls the
// production formUploadViaButton with a trusted Rod click and verifies the file is delivered.
// The button's click listener synchronously creates an <input type=file> and calls f.click().
// The change handler creates a checked radio with the uploaded filename, simulating LinkedIn's
// actual behavior after a successful upload.
func TestFormUploadViaButton_TrustedClick_DeliversFile(t *testing.T) {
	const html = `<!DOCTYPE html><html><body>
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
    <footer><button type="button"><span>Next</span></button></footer>
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
	err := b.formUploadViaButton(context.Background(), page, tmpFile)
	require.NoError(t, err, "trusted click must deliver file and confirm radio selection")

	// Verify the radio with the correct filename is present and checked.
	res, err := page.Eval(jsLinkedInCheckResumeAccepted, "my_tailored_resume.pdf")
	require.NoError(t, err)
	require.True(t, res.Value.Get("accepted").Bool())
	require.True(t, res.Value.Get("filenameVisible").Bool())
}

// TestFormUploadViaButton_DuplicateFilename_Unverifiable verifies that when the expected
// filename was already selected before upload and the section radio count does not increase
// after upload, errUploadIdentityUnverifiable is returned rather than a false success.
func TestFormUploadViaButton_DuplicateFilename_Unverifiable(t *testing.T) {
	// Resume section already has "my_tailored_resume.pdf" checked.
	// Upload button click creates a file input but the change handler does NOT
	// add a new radio — simulating LinkedIn reusing the existing library entry.
	const html = `<!DOCTYPE html><html><body>
<dialog data-testid="dialog" open="">
  <div data-sdui-screen="com.linkedin.sdui.flagshipnav.jobs.easyapply.EasyApply">
    <fieldset id="resume-section">
      <legend>Resume <span aria-hidden="true">*</span></legend>
      <div>
        <input type="radio" name="resume-select" id="r-existing" checked>
        <label for="r-existing">my_tailored_resume.pdf</label>
      </div>
      <button type="button" id="upload-btn">
        <span><span>Upload resume</span></span>
      </button>
    </fieldset>
    <footer><button type="button"><span>Next</span></button></footer>
  </div>
</dialog>
<script>
document.getElementById('upload-btn').addEventListener('click', function() {
  var f = document.createElement('input');
  f.type = 'file';
  f.style.display = 'none';
  // change handler intentionally does NOT add a new radio (duplicate scenario)
  f.addEventListener('change', function() { f.remove(); });
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
	err := b.formUploadViaButton(context.Background(), page, tmpFile)
	require.True(t, errors.Is(err, errUploadIdentityUnverifiable),
		"pre-existing same filename with no new radio must return errUploadIdentityUnverifiable, got: %v", err)
}

// TestLinkedInFinalReviewResumeCheck_HeadingAndFilename is a regression test for the
// heading-bleeds-into-filename bug. When the review section has an <h3>Resume</h3>
// heading and the filename as a sibling <p>, section.textContent would have yielded
// "Resume\nMy Resume 2026.pdf" causing extractFilename to misidentify the string.
// After the fix only non-heading children are searched, so the correct filename is found.
func TestLinkedInFinalReviewResumeCheck_HeadingAndFilename(t *testing.T) {
	const html = `<!DOCTYPE html><html><body>
<dialog data-testid="dialog" open="">
  <div data-sdui-screen="com.linkedin.sdui.flagshipnav.jobs.easyapply.EasyApply">
    <h2>Review your application</h2>
    <section data-component-type="ReviewSection">
      <h3>Resume</h3>
      <p>My Resume 2026.pdf</p>
    </section>
    <button data-easy-apply-submit-button="">Submit application</button>
  </div>
</dialog>
</body></html>`
	page := rodTestPage(t, html)

	res, err := page.Eval(jsLinkedInFinalReviewResumeCheck, "My Resume 2026.pdf")
	require.NoError(t, err)
	require.True(t, res.Value.Get("verified").Bool(),
		"filename in non-heading element must be verified even when heading text precedes it in section")

	// Wrong filename must still fail.
	res2, err2 := page.Eval(jsLinkedInFinalReviewResumeCheck, "other_resume.pdf")
	require.NoError(t, err2)
	require.False(t, res2.Value.Get("verified").Bool())
}

// TestFormUploadViaButton_UnselectedDuplicate_Unverifiable tests that a pre-existing
// UNCHECKED radio entry with the same filename triggers the duplicate guard.
// LinkedIn may silently select an existing library entry rather than uploading a new
// file — this is indistinguishable from a fresh upload without the all-entry check.
func TestFormUploadViaButton_UnselectedDuplicate_Unverifiable(t *testing.T) {
	const html = `<!DOCTYPE html><html><body>
<dialog data-testid="dialog" open="">
  <div data-sdui-screen="com.linkedin.sdui.flagshipnav.jobs.easyapply.EasyApply">
    <fieldset id="resume-section">
      <legend>Resume <span aria-hidden="true">*</span></legend>
      <div>
        <input type="radio" name="resume-select" id="r-existing">
        <label for="r-existing">my_tailored_resume.pdf</label>
      </div>
      <button type="button" id="upload-btn">
        <span><span>Upload resume</span></span>
      </button>
    </fieldset>
    <footer><button type="button"><span>Next</span></button></footer>
  </div>
</dialog>
<script>
document.getElementById('upload-btn').addEventListener('click', function() {
  var f = document.createElement('input');
  f.type = 'file';
  f.style.display = 'none';
  f.addEventListener('change', function() {
    // Simulate LinkedIn selecting the existing entry instead of adding a new one.
    document.getElementById('r-existing').checked = true;
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
	err := b.formUploadViaButton(context.Background(), page, tmpFile)
	require.True(t, errors.Is(err, errUploadIdentityUnverifiable),
		"pre-existing unselected entry with same filename must be caught, got: %v", err)
}

// nativeDialogShell wraps inner HTML in the outer native-dialog structure so
// resolveApplyContainer() works in JS eval tests.
func nativeDialogShell(inner string) string {
	return `<!DOCTYPE html><html><body>
<dialog data-testid="dialog" open="">
  <div data-sdui-screen="com.linkedin.sdui.flagshipnav.jobs.easyapply.EasyApply">
` + inner + `
  </div>
</dialog>
</body></html>`
}

// TestLinkedInResumePreUploadState_ContainerOnlyLayout verifies that when radios and the
// upload wrapper share only the application container (no named fieldset/section ancestor
// of the button), the function returns stateVerified:false with sectionFound:false — so
// the Go caller holds immediately without retrying.
func TestLinkedInResumePreUploadState_ContainerOnlyLayout(t *testing.T) {
	html := nativeDialogShell(`
    <div class="radio-wrapper">
      <input type="radio" id="r1"><label for="r1">old.pdf</label>
    </div>
    <div class="upload-wrapper">
      <button><span><span>Upload resume</span></span></button>
    </div>`)
	page := rodTestPage(t, html)

	res, err := page.Eval(jsLinkedInResumePreUploadState, "old.pdf")
	require.NoError(t, err)
	require.False(t, res.Value.Get("stateVerified").Bool(), "unresolved section must not be verified")
	require.False(t, res.Value.Get("sectionFound").Bool(), "sectionFound must be false when walk reaches container")
}

// TestLinkedInResumePreUploadState_EmptyFieldset verifies that when the upload button is
// inside a fieldset with no radios (possibly still loading), the function returns
// stateVerified:false with sectionFound:true — so the Go caller retries before concluding
// it is a confirmed empty library.
func TestLinkedInResumePreUploadState_EmptyFieldset(t *testing.T) {
	html := nativeDialogShell(`
    <fieldset id="resume-section">
      <legend>Resume <span aria-hidden="true">*</span></legend>
      <button><span><span>Upload resume</span></span></button>
    </fieldset>`)
	page := rodTestPage(t, html)

	res, err := page.Eval(jsLinkedInResumePreUploadState, "any_resume.pdf")
	require.NoError(t, err)
	require.False(t, res.Value.Get("stateVerified").Bool(), "empty fieldset must not return stateVerified:true immediately")
	require.True(t, res.Value.Get("sectionFound").Bool(), "sectionFound must be true when fieldset is identified")
	require.False(t, res.Value.Get("filenameExisted").Bool())
}

// TestLinkedInResumePreUploadState_SeparateWrappersInFieldset verifies that when radios
// and the upload button are in sibling <div> wrappers inside a common <fieldset>, duplicate
// detection still works correctly (the fieldset is found via the FIELDSET tag check during
// the walk, not via a radio-containing ancestor of the button).
func TestLinkedInResumePreUploadState_SeparateWrappersInFieldset(t *testing.T) {
	html := nativeDialogShell(`
    <fieldset id="resume-section">
      <legend>Resume <span aria-hidden="true">*</span></legend>
      <div class="radio-wrapper">
        <input type="radio" id="r-lib1" name="resume-select">
        <label for="r-lib1">existing_resume.pdf</label>
      </div>
      <div class="upload-wrapper">
        <button><span><span>Upload resume</span></span></button>
      </div>
    </fieldset>`)
	page := rodTestPage(t, html)

	t.Run("duplicate_detected", func(t *testing.T) {
		res, err := page.Eval(jsLinkedInResumePreUploadState, "existing_resume.pdf")
		require.NoError(t, err)
		require.True(t, res.Value.Get("stateVerified").Bool(), "radios in sibling wrapper must still be found via fieldset")
		require.True(t, res.Value.Get("filenameExisted").Bool(), "matching filename in sibling radio wrapper must be detected")
		require.True(t, res.Value.Get("sectionFound").Bool())
	})

	t.Run("fresh_upload", func(t *testing.T) {
		res, err := page.Eval(jsLinkedInResumePreUploadState, "new_resume.pdf")
		require.NoError(t, err)
		require.True(t, res.Value.Get("stateVerified").Bool())
		require.False(t, res.Value.Get("filenameExisted").Bool(), "different filename must not trigger duplicate guard")
		require.True(t, res.Value.Get("sectionFound").Bool())
	})
}

// TestLinkedInResumePreUploadState_TestdataFixture runs jsLinkedInResumePreUploadState
// against testdata/linkedin/native-dialog-resume.html. The file was described as the
// Oct 22 2026 capture but was created/edited during this PR, so its provenance cannot
// be independently verified here. The test pins the current known behaviour against
// the file as it stands.
//
// Known content: one unchecked radio with label "my_resume_2026.pdf".
func TestLinkedInResumePreUploadState_TestdataFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/linkedin/native-dialog-resume.html")
	require.NoError(t, err, "testdata file must be readable")

	page := rodTestPage(t, string(raw))

	// Filename that matches the library entry — must be detected as an existing entry.
	res, evalErr := page.Eval(jsLinkedInResumePreUploadState, "my_resume_2026.pdf")
	require.NoError(t, evalErr)
	require.True(t, res.Value.Get("stateVerified").Bool(), "section with radio must be verified")
	require.True(t, res.Value.Get("sectionFound").Bool())
	require.True(t, res.Value.Get("filenameExisted").Bool(), "matching filename must be detected as duplicate")

	// Different filename — section still verified, no duplicate.
	res2, evalErr2 := page.Eval(jsLinkedInResumePreUploadState, "other_resume.pdf")
	require.NoError(t, evalErr2)
	require.True(t, res2.Value.Get("stateVerified").Bool())
	require.True(t, res2.Value.Get("sectionFound").Bool())
	require.False(t, res2.Value.Get("filenameExisted").Bool(), "different filename must not trigger duplicate guard")
}

// TestFormUploadViaButton_EmptyFieldset_HoldsAfterRetries verifies that when the resume
// section is consistently present but has no radio entries after all retries,
// formUploadViaButton returns errUploadIdentityUnverifiable and never clicks the upload
// button. Empty or unverifiable libraries are held for manual review.
func TestFormUploadViaButton_EmptyFieldset_HoldsAfterRetries(t *testing.T) {
	const html = `<!DOCTYPE html><html><body>
<dialog data-testid="dialog" open="">
  <div data-sdui-screen="com.linkedin.sdui.flagshipnav.jobs.easyapply.EasyApply">
    <fieldset id="resume-section">
      <legend>Resume <span aria-hidden="true">*</span></legend>
      <button type="button" id="upload-btn" onclick="window._uploadBtnClicked=true;">
        <span><span>Upload resume</span></span>
      </button>
    </fieldset>
  </div>
</dialog>
</body></html>`

	br := launchTestBrowser(t)
	page := br.MustPage("")
	t.Cleanup(func() { _ = page.Close() })
	require.NoError(t, page.SetDocumentContent(html))

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "my_tailored_resume.pdf")
	require.NoError(t, os.WriteFile(tmpFile, []byte("%PDF-1.4"), 0644))

	b := &Bot{cfg: Config{}, seenCache: newJobSeenCache()}
	err := b.formUploadViaButton(context.Background(), page, tmpFile)
	require.True(t, errors.Is(err, errUploadIdentityUnverifiable),
		"empty fieldset after retries must hold, got: %v", err)

	// Upload button must not have been clicked while library state remained unresolved.
	clicked, evalErr := page.Eval(`() => !!window._uploadBtnClicked`)
	require.NoError(t, evalErr)
	require.False(t, clicked.Value.Bool(), "upload button must not be clicked when state never verified")
}

// TestFormUploadViaButton_RadiosLoadDuringRetry verifies the retry loop succeeds when
// the resume library section starts empty (possibly still loading) and an entry appears
// between the first and second probe.
//
// Synchronisation: the page intercepts querySelectorAll('input[type="radio"]') to track
// probe count. The test goroutine waits for _radioProbeCount >= 1 (first probe observed
// unresolved state), asserts no upload click yet, then injects a library entry before the
// second probe fires. A bounded context ensures the worker goroutine is cancelled and
// joined on any test failure.
func TestFormUploadViaButton_RadiosLoadDuringRetry(t *testing.T) {
	const html = `<!DOCTYPE html><html><body>
<dialog data-testid="dialog" open="">
  <div data-sdui-screen="com.linkedin.sdui.flagshipnav.jobs.easyapply.EasyApply">
    <fieldset id="resume-section">
      <legend>Resume <span aria-hidden="true">*</span></legend>
      <button type="button" id="upload-btn" onclick="window._uploadBtnClicked=true;">
        <span><span>Upload resume</span></span>
      </button>
    </fieldset>
  </div>
</dialog>
<script>
// Intercept querySelectorAll('input[type="radio"]') to count production probes.
(function() {
  var orig = Element.prototype.querySelectorAll;
  Element.prototype.querySelectorAll = function(sel) {
    if (sel === 'input[type="radio"]') window._radioProbeCount = (window._radioProbeCount || 0) + 1;
    return orig.call(this, sel);
  };
})();
// _injectLibraryEntry inserts a pre-existing library radio the test calls on demand.
window._injectLibraryEntry = function() {
  var wrap = document.createElement('div');
  var r = document.createElement('input');
  r.type = 'radio'; r.name = 'resume-select'; r.id = 'r-lib1';
  var lbl = document.createElement('label');
  lbl.htmlFor = 'r-lib1'; lbl.textContent = 'library_resume.pdf';
  wrap.appendChild(r); wrap.appendChild(lbl);
  var btn = document.getElementById('upload-btn');
  btn.parentElement.insertBefore(wrap, btn);
};
// Upload button click handler: create file input, add checked radio on change.
document.getElementById('upload-btn').addEventListener('click', function() {
  var f = document.createElement('input');
  f.type = 'file'; f.style.display = 'none';
  f.addEventListener('change', function() {
    var name = f.files[0] ? f.files[0].name : 'unknown.pdf';
    var wrap = document.createElement('div');
    var radio = document.createElement('input');
    radio.type = 'radio'; radio.name = 'resume-select'; radio.id = 'r-uploaded'; radio.checked = true;
    var label = document.createElement('label');
    label.htmlFor = 'r-uploaded'; label.textContent = name;
    wrap.appendChild(radio); wrap.appendChild(label);
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

	// Bounded context: ensures the worker goroutine is cancelled and joined even if
	// an assertion fails before the goroutine completes.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// complete is closed (not sent on) when formUploadViaButton exits. Receiving on a
	// closed channel returns immediately, so both the test body and the cleanup can safely
	// wait on it without double-draining a buffered channel.
	complete := make(chan struct{})
	var uploadResult error
	go func() {
		uploadResult = (&Bot{cfg: Config{}, seenCache: newJobSeenCache()}).formUploadViaButton(ctx, page, tmpFile)
		close(complete)
	}()
	t.Cleanup(func() {
		cancel()
		<-complete // join goroutine; safe even after test body already received from complete
	})

	// Wait for the first production probe to check for radios — explicit synchronisation,
	// not a wall-clock timer.
	require.Eventually(t,
		func() bool {
			r, err := page.Timeout(2 * time.Second).Eval(`() => (window._radioProbeCount || 0)`)
			return err == nil && r.Value.Num() >= 1
		},
		5*time.Second, 10*time.Millisecond,
		"first radio probe must fire within timeout",
	)

	// First probe observed unresolved state — upload button must not have been clicked.
	clicked, evalErr := page.Timeout(2 * time.Second).Eval(`() => !!window._uploadBtnClicked`)
	require.NoError(t, evalErr)
	require.False(t, clicked.Value.Bool(), "upload button must not be clicked before library entry is injected")

	// Inject the library entry. The next probe (after the 500 ms retry wait) will find it.
	_, injectErr := page.Timeout(2 * time.Second).Eval(`() => window._injectLibraryEntry()`)
	require.NoError(t, injectErr)

	// Wait for the upload to complete.
	select {
	case <-complete:
		require.NoError(t, uploadResult, "upload must succeed after library entry is injected during retry")
	case <-ctx.Done():
		t.Fatal("formUploadViaButton did not complete within timeout after library entry injection")
	}

	// Verify the uploaded file was accepted.
	res, err := page.Timeout(2 * time.Second).Eval(jsLinkedInCheckResumeAccepted, "my_tailored_resume.pdf")
	require.NoError(t, err)
	require.True(t, res.Value.Get("accepted").Bool())
	require.True(t, res.Value.Get("filenameVisible").Bool())
}
