package bot

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/documents"
)

func TestApplyStoredPackMetadata_LoadsSiteSkipWhenNotFrozen(t *testing.T) {
	t.Parallel()
	packJSON := documents.WriteApplicationPackJSON(documents.ApplicationDocumentPack{
		Selection: documents.ApplicationDocumentSelection{ResumeUseSite: true, CoverSkip: true},
		HoldReason: "Document selection changed — prepare again to preview",
	})
	l := &lazyDocGen{refsJSON: packJSON, frozen: false}
	l.applyStoredPackMetadata()
	o := l.applicationDocumentOverrides()
	require.True(t, o.ResumeUseSite)
	require.True(t, o.CoverSkip)
}

func TestNewManagerLazy_PreservesSiteSkipForPrepare(t *testing.T) {
	t.Parallel()
	packJSON := documents.WriteApplicationPackJSON(documents.ApplicationDocumentPack{
		Selection: documents.ApplicationDocumentSelection{ResumeUseSite: true, CoverSkip: true},
		HoldReason: "Document selection changed — prepare again to preview",
	})
	req := SubmitRequest{
		JobID: "j1", Company: "Co", Role: "Role", DocumentRefsJSON: packJSON,
	}
	l := newManagerLazy(&Bot{}, t.Context(), req)
	o := l.applicationDocumentOverrides()
	require.True(t, o.ResumeUseSite)
	require.True(t, o.CoverSkip)
}
