package bot

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/documents"
	"github.com/user/jobifai/internal/domain"
)

// TestPrepareResolution_UsesSiteSkipFromStoredPack verifies unfrozen prepare reads picker flags.
func TestPrepareResolution_UsesSiteSkipFromStoredPack(t *testing.T) {
	t.Parallel()
	b := testBotWithProfile()
	b.cfg.Settings.DocumentPolicies.ResumeMode = domain.ResumeDocumentModeDefault
	b.cfg.Settings.DocumentPolicies.CoverMode = domain.CoverDocumentModeWhenRequired
	packJSON := documents.WriteApplicationPackJSON(documents.ApplicationDocumentPack{
		Selection: documents.ApplicationDocumentSelection{ResumeUseSite: true, CoverSkip: true},
	})
	l := newManagerLazy(b, t.Context(), SubmitRequest{JobID: "j", DocumentRefsJSON: packJSON})
	l.formCaps = documents.FormDocumentCapabilities{
		Detected: true, SiteResumePresent: true, CoverOptional: true,
	}
	res := b.resolveApplicationDocs(l.applicationDocumentOverrides(), l.formCaps)
	require.True(t, res.Resume.UseSite)
	require.True(t, res.Cover.Skip)
}
