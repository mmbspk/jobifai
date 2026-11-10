package bot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLinkedInFinalReviewResume_CapturedAttachmentCard(t *testing.T) {
	raw, err := os.ReadFile("testdata/linkedin/native-dialog-div-final-review.html")
	require.NoError(t, err)
	// Captured div hierarchy and attachment card; contact answers and URL attributes removed.
	for _, tc := range []struct {
		name, html, observed string
		verified             bool
	}{
		{"wrong_attachment", string(raw), "cover.pdf", false},
		{"intended_attachment", strings.ReplaceAll(string(raw), "Cover.pdf", "resume_8c080411.pdf"), "resume_8c080411.pdf", true},
		{"missing_attachment", strings.ReplaceAll(string(raw), "Cover.pdf", "No file selected"), "", false},
		{"filename_in_other_section", strings.ReplaceAll(strings.ReplaceAll(string(raw), "Cover.pdf", "No file selected"), "[REDACTED]", "resume_8c080411.pdf"), "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page := rodTestPage(t, tc.html)
			r, err := page.Eval(jsLinkedInFinalReviewResumeCheck, "resume_8c080411.pdf")
			require.NoError(t, err)
			require.Equal(t, tc.verified, r.Value.Get("verified").Bool())
			if tc.observed == "" {
				require.True(t, r.Value.Get("observedFilename").Nil())
			} else {
				require.Equal(t, tc.observed, r.Value.Get("observedFilename").String())
			}
			if tc.name == "wrong_attachment" {
				err = (&Bot{}).linkedInVerifyFinalReviewResume(page, "resume_8c080411.pdf")
				require.ErrorContains(t, err, "cover.pdf")
				require.ErrorContains(t, err, "resume_8c080411.pdf")
			}
		})
	}
}

// A file chooser that never opens returns a cancellation error. Preparation must
// preserve it and leave the existing library selection untouched, with no Next click.
func TestScanApplyStepsForPrepare_UploadErrorDoesNotAdvance(t *testing.T) {
	br := launchTestBrowser(t)
	const jobID = "prepare-upload-error"
	const html = `<dialog open data-job-id="prepare-upload-error">
 <div data-sdui-screen="com.linkedin.sdui.flagshipnav.jobs.easyapply.EasyApply">
 <fieldset><legend>Resume</legend>
 <input type="radio" name="resume" aria-label="Cover.pdf" checked>
 <button onclick="window._uploadClicked=true">Upload resume</button></fieldset>
 <footer><button onclick="window._nextClicks=(window._nextClicks||0)+1">Next</button></footer>
 </div></dialog>`
	srv := fixtureServer(t, jobID, "", html, "")
	page := navigateAndWait(t, br, srv.URL+"/jobs/view/"+jobID)
	path := filepath.Join(t.TempDir(), "resume_8c080411.pdf")
	require.NoError(t, os.WriteFile(path, []byte("%PDF-1.4"), 0600))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := &Bot{cfg: Config{}, seenCache: newJobSeenCache()}
	b.cfg.Settings.GenerateNewResumeDocs = true
	lazy := &lazyDocGen{b: b, job: linkedInJob{ID: jobID, URL: srv.URL + "/jobs/view/" + jobID}, ctx: ctx, reviewPrepareOnly: true, resumeOverride: path}
	result := make(chan error, 1)
	go func() { result <- b.scanApplyStepsForPrepare(ctx, page, lazy) }()
	require.Eventually(t, func() bool {
		r, err := page.Eval(`() => !!window._uploadClicked`)
		return err == nil && r.Value.Bool()
	}, 10*time.Second, 20*time.Millisecond)
	cancel()
	select {
	case err := <-result:
		require.ErrorIs(t, err, context.Canceled)
		require.ErrorContains(t, err, "form step 1 failed")
	case <-time.After(5 * time.Second):
		t.Fatal("prepare did not return after cancelled upload")
	}
	r, err := page.Eval(`() => window._nextClicks || 0`)
	require.NoError(t, err)
	require.Zero(t, r.Value.Int(), "failed upload must not advance with old attachment")
	require.False(t, lazy.prepareScanComplete)
}
