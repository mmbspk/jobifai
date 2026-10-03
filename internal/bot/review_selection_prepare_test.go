package bot

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	appdb "github.com/user/jobifai/internal/db"
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

func TestPickerChangeThenMaterialize_ReadyForSubmit(t *testing.T) {
	t.Parallel()
	b := testBotWithProfile()
	b.cfg.UserID = "user-prepare-hold"
	db, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	b.cfg.Documents = &documents.Service{Store: documents.NewStore(db)}
	packJSON := documents.WriteApplicationPackJSON(documents.PackAfterSelectionChange(documents.ApplicationDocumentSelection{
		ResumeUseSite: true,
		CoverSkip:     true,
	}))
	l := &lazyDocGen{
		b: b, ctx: context.Background(),
		job: linkedInJob{ID: "job-hold-clear", Title: "Role", Company: "Co"},
		refsJSON: packJSON, jobDesc: "Role at Co",
	}
	l.applyStoredPackMetadata()
	require.Empty(t, l.holdReason, "stale picker hold must not block a new prepare attempt")
	l.formCaps = documents.FormDocumentCapabilities{
		Detected: true, SiteResumePresent: true, CoverOptional: true,
	}
	l.materializeWithPolicies()
	require.False(t, l.policyBlocked())
	require.Empty(t, l.holdReason)

	pack := documents.ParseApplicationPackJSON(l.refsJSON)
	if !l.policyBlocked() {
		pack.MarkPrepared()
	}
	pack.Selection = documents.ApplicationDocumentSelection{ResumeUseSite: true, CoverSkip: true}
	require.True(t, pack.ReadyForSubmit(), "successful prepare after picker change must allow approve")
}

func TestApplyStoredPackMetadata_KeepsNonStaleHold(t *testing.T) {
	t.Parallel()
	packJSON := documents.WriteApplicationPackJSON(documents.ApplicationDocumentPack{
		HoldReason: "AI quota exceeded for this session",
	})
	l := &lazyDocGen{refsJSON: packJSON}
	l.applyStoredPackMetadata()
	require.Equal(t, "AI quota exceeded for this session", l.holdReason)
}
