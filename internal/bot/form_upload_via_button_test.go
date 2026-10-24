package bot

import (
	"context"
	"errors"
	"testing"

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
	page := rodTestPage(t, nativeDialogWithResume("other_resume.pdf", false))

	err := linkedInWaitForResumeAccepted(context.Background(), page, "/uploads/my_resume_2026.pdf")
	require.True(t, errors.Is(err, errUploadIdentityUnverifiable),
		"mismatched filename must return errUploadIdentityUnverifiable, got: %v", err)
}

// TestLinkedInWaitForResumeAccepted_AcceptsCorrectFile verifies that when the checked
// radio label matches the expected filename, nil is returned.
func TestLinkedInWaitForResumeAccepted_AcceptsCorrectFile(t *testing.T) {
	// Page has "my_resume_2026.pdf" checked; same filename is expected.
	page := rodTestPage(t, nativeDialogWithResume("my_resume_2026.pdf", false))

	err := linkedInWaitForResumeAccepted(context.Background(), page, "/uploads/my_resume_2026.pdf")
	require.NoError(t, err, "matching filename must be accepted without error")
}
