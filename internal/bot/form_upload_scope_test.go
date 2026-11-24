package bot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormUploadFile_UsesCurrentScannedDocumentScope(t *testing.T) {
	for _, withID := range []bool{true, false} {
		t.Run(fmt.Sprintf("resume ID=%t", withID), func(t *testing.T) {
			resumeID := ""
			if withID {
				resumeID = `id="resume"`
			}
			page := rodTestPage(t, fmt.Sprintf(`<html><body>
 <form><input id="outside" type="file" accept=".pdf" data-rod-upload-target="0"></form>
 <div role="dialog" class="jobs-easy-apply-modal">
  <fieldset style="opacity:0"><label>Resume<input id="old-step" type="file" accept=".pdf"></label></fieldset>
  <fieldset><legend>Documents</legend>
   <label>Photo<input id="photo" type="file" accept="image/png"></label>
   <label>Resume<input %s data-purpose="resume" type="file" name="resume" accept=".pdf" required></label>
   <label>Cover letter<input id="cover" type="file" accept=".pdf"></label>
  </fieldset>
  <button>Continue</button>
 </div></body></html>`, resumeID))
			fields := evalScanFields(t, page)
			require.Len(t, fields, 2)
			require.Equal(t, 0, fields[0].Index)
			require.Equal(t, 1, fields[1].Index)
			b := newTestBot(t)
			for i, name := range []string{"approved-resume.pdf", "approved-cover.pdf"} {
				path := filepath.Join(t.TempDir(), name)
				require.NoError(t, os.WriteFile(path, []byte("%PDF-1.4 scoped upload fixture"), 0600))
				require.NoError(t, b.formUploadFile(page, path, fields[i].Index, fields[i].ID))
			}
			state, err := page.Eval(`() => ({
 outside: document.getElementById('outside').files.length,
 old: document.getElementById('old-step').files.length,
 photo: document.getElementById('photo').files.length,
 resume: document.querySelector('[data-purpose="resume"]').files[0]?.name,
 cover: document.getElementById('cover').files[0]?.name,
 markers: document.querySelectorAll('[data-rod-upload-target]').length
})`)
			require.NoError(t, err)
			require.Zero(t, state.Value.Get("outside").Int())
			require.Zero(t, state.Value.Get("old").Int())
			require.Zero(t, state.Value.Get("photo").Int())
			require.Equal(t, "approved-resume.pdf", state.Value.Get("resume").String())
			require.Equal(t, "approved-cover.pdf", state.Value.Get("cover").String())
			require.Zero(t, state.Value.Get("markers").Int())
		})
	}
}

func TestFormUploadFile_MissingOrAmbiguousTargetDoesNotUpload(t *testing.T) {
	for _, tc := range []struct{ name, html, id string }{
		{"missing scope", `<form><label>Resume<input id="resume" type="file" accept=".pdf"></label></form>`, "resume"},
		{"ambiguous scope", `<dialog open><div data-sdui-screen="EasyApply"><input id="resume" type="file" accept=".pdf"></div></dialog><dialog open><div data-sdui-screen="EasyApply"><input type="file" accept=".pdf"></div></dialog>`, "resume"},
		{"ambiguous legacy scope", `<div class="jobs-easy-apply-modal"><fieldset><label>Resume<input id="resume" type="file" accept=".pdf"></label></fieldset></div><div class="jobs-easy-apply-modal"><fieldset><label>Resume<input id="other" type="file" accept=".pdf"></label></fieldset></div>`, "resume"},
		{"duplicate ID", `<div class="jobs-easy-apply-modal"><fieldset><label>Resume<input id="resume" type="file" accept=".pdf"></label><label>Cover letter<input id="resume" type="file" accept=".pdf"></label></fieldset></div>`, "resume"},
		{"stale ID", `<div class="jobs-easy-apply-modal"><fieldset><label>Resume<input id="current" type="file" accept=".pdf"></label></fieldset></div>`, "resume"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page := rodTestPage(t, "<html><body>"+tc.html+"</body></html>")
			path := filepath.Join(t.TempDir(), "approved-resume.pdf")
			require.NoError(t, os.WriteFile(path, []byte("%PDF-1.4 scoped upload fixture"), 0600))
			require.Error(t, newTestBot(t).formUploadFile(page, path, 0, tc.id))
			state, err := page.Eval(`() => [...document.querySelectorAll('input[type="file"]')].reduce((n, el) => n + el.files.length, 0)`)
			require.NoError(t, err)
			require.Zero(t, state.Value.Int())
		})
	}
}

func TestAdaptiveUpload_NoIDUsesScannedDocumentInputIndex(t *testing.T) {
	const target = "https://boards.greenhouse.io/acmeinc/jobs/4567890"
	page := ghHijackPage(t, func(string) string {
		return `<html><body>
 <form><input id="outside" type="file" accept=".pdf"></form>
 <form id="application_form">
  <label for="name">Full Name</label><input id="name" name="full_name">
  <fieldset><legend>Documents</legend>
   <label>Photo<input id="photo" type="file" accept="image/png"></label>
   <label>Resume<input data-purpose="resume" type="file" name="resume" accept=".pdf" required></label>
  </fieldset>
  <button type="submit">Submit application</button>
 </form></body></html>`
	})
	require.NoError(t, page.Navigate(target))
	require.NoError(t, page.WaitLoad())
	b := newTestBot(t)
	path := filepath.Join(t.TempDir(), "approved-resume.pdf")
	require.NoError(t, os.WriteFile(path, []byte("%PDF-1.4 scoped upload fixture"), 0600))
	lazy := &lazyDocGen{b: b, ctx: context.Background(), frozen: true, packRestored: true, resume: path}
	fields, _, err := b.scanAdaptiveStep(context.Background(), page)
	require.NoError(t, err)
	snap := captureStepSnapshot(fields, nil, "greenhouse", 0, "Application")
	uploads := 0
	for _, field := range snap.Fields {
		if field.BrowserField == nil || field.BrowserField.Type != "file" {
			continue
		}
		require.Empty(t, field.BrowserField.ID)
		result := (&greenhouseRodFiller{page: page, bot: b, lazy: lazy}).Fill(context.Background(), field, FieldResolution{Ref: field.Ref, Action: ActionUpload})
		require.NoError(t, result.Err)
		require.True(t, result.Filled)
		uploads++
	}
	require.Equal(t, 1, uploads)
	state, err := page.Eval(`() => ({
 outside: document.getElementById('outside').files.length,
 photo: document.getElementById('photo').files.length,
 resume: document.querySelector('[data-purpose="resume"]').files[0]?.name
})`)
	require.NoError(t, err)
	require.Zero(t, state.Value.Get("outside").Int())
	require.Zero(t, state.Value.Get("photo").Int())
	require.Equal(t, "approved-resume.pdf", state.Value.Get("resume").String())
}
