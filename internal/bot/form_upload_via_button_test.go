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
    <div id="resume-section">
      <label>Resume <span aria-hidden="true">*</span></label>
      ` + radioChecked + `
      ` + uploadBtn + `
    </div>
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
    <div id="resume-section">
      <label>Resume <span aria-hidden="true">*</span></label>
      <button type="button" id="upload-btn" onclick="setTimeout(function(){var f=document.createElement('input');f.type='file';f.id='r-chooser';document.body.appendChild(f);f.click();},500)">
        <span><span>Upload resume</span></span>
      </button>
    </div>
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
    <div id="resume-section">
      <label>Resume <span aria-hidden="true">*</span></label>
      <button type="button" id="upload-btn">
        <span><span>Upload resume</span></span>
      </button>
    </div>
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
    <div id="resume-section">
      <label>Resume <span aria-hidden="true">*</span></label>
      <div>
        <input type="radio" name="resume-select" id="r-existing" checked>
        <label for="r-existing">my_tailored_resume.pdf</label>
      </div>
      <button type="button" id="upload-btn">
        <span><span>Upload resume</span></span>
      </button>
    </div>
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
    <div id="resume-section">
      <label>Resume <span aria-hidden="true">*</span></label>
      <div>
        <input type="radio" name="resume-select" id="r-existing">
        <label for="r-existing">my_tailored_resume.pdf</label>
      </div>
      <button type="button" id="upload-btn">
        <span><span>Upload resume</span></span>
      </button>
    </div>
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
