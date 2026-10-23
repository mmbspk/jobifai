package bot

import (
	"encoding/json"
	"testing"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/documents"
)

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
