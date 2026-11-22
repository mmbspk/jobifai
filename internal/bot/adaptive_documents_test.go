package bot

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/documents"
)

func TestAdaptiveUpload_AttachesApprovedFilesByScannedIdentity(t *testing.T) {
	const target = "https://boards.greenhouse.io/acmeinc/jobs/4567890"
	page := ghHijackPage(t, func(string) string {
		return `<html><body>
 <form><input type="file" id="unrelated"></form>
 <form id="application_form">
 <label for="name">Full Name</label><input id="name" name="full_name">
 <label for="resume">Resume</label><input type="file" id="resume" name="resume" accept=".pdf" required>
 <label for="cover">Cover Letter</label><input type="file" id="cover" name="cover" accept=".pdf" required>
 <button type="submit">Submit application</button></form></body></html>`
	})
	require.NoError(t, page.Navigate(target))
	require.NoError(t, page.WaitLoad())
	b := newTestBot(t)
	dir := t.TempDir()
	resumePath, coverPath := filepath.Join(dir, "approved-resume.pdf"), filepath.Join(dir, "approved-cover.pdf")
	require.NoError(t, os.WriteFile(resumePath, []byte("%PDF-1.4 fixture resume"), 0600))
	require.NoError(t, os.WriteFile(coverPath, []byte("%PDF-1.4 fixture cover"), 0600))
	lazy := &lazyDocGen{b: b, ctx: context.Background(), frozen: true, packRestored: true, resume: resumePath, cover: coverPath}
	fields, _, err := b.scanAdaptiveStep(context.Background(), page)
	require.NoError(t, err)
	// Name before upload means the opaque ref ordinal differs from file ordinal.
	snap := captureStepSnapshot(fields, nil, "greenhouse", 0, "Application")
	filler := &greenhouseRodFiller{page: page, bot: b, lazy: lazy}
	count := 0
	for _, field := range snap.Fields {
		if field.Type != SnapTypeFile {
			continue
		}
		got := filler.Fill(context.Background(), field, FieldResolution{Ref: field.Ref, Action: ActionUpload})
		require.NoError(t, got.Err)
		require.True(t, got.Filled)
		count++
	}
	require.Equal(t, 2, count)
	values, err := page.Eval(`() => ({resume: document.getElementById('resume').files[0]?.name, cover: document.getElementById('cover').files[0]?.name, unrelated: document.getElementById('unrelated').files.length})`)
	require.NoError(t, err)
	assert.Equal(t, "approved-resume.pdf", values.Value.Get("resume").String())
	assert.Equal(t, "approved-cover.pdf", values.Value.Get("cover").String())
	assert.Zero(t, values.Value.Get("unrelated").Int())
}

func TestAdaptiveUpload_MissingApprovedDocumentCannotReportFilled(t *testing.T) {
	b := newTestBot(t)
	lazy := &lazyDocGen{b: b, frozen: true, packRestored: true}
	f := formField{Type: "file", ID: "resume", DocumentKind: documents.KindResume, Required: true}
	result := b.fillAdaptiveDocument(context.Background(), nil, lazy, SnapField{Ref: "f5", BrowserField: &f})
	require.Error(t, result.Err)
	assert.False(t, result.Filled)
}
