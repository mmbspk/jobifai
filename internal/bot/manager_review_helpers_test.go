package bot

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/documents"
)

func TestApplyStoredPackMetadata_LoadsSiteSkipWhenNotFrozen(t *testing.T) {
	t.Parallel()
	packJSON := documents.WriteApplicationPackJSON(documents.ApplicationDocumentPack{
		Selection:  documents.ApplicationDocumentSelection{ResumeUseSite: true, CoverSkip: true},
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
		Selection:  documents.ApplicationDocumentSelection{ResumeUseSite: true, CoverSkip: true},
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

func TestNewManagerLazy_PreservesDeclaredJobIdentity(t *testing.T) {
	t.Parallel()
	req := SubmitRequest{
		JobID: "internal-application-id", Link: "https://www.linkedin.com/jobs/view/4479748087/",
		Company: "MYOB", Role: "Principal Developer", Location: "Melbourne",
	}
	lazy := newManagerLazy(&Bot{}, t.Context(), req)
	require.Equal(t, req.Link, lazy.job.URL)
	require.Equal(t, req.JobID, lazy.job.ID)
	require.Equal(t, req.Location, lazy.job.Location)
	require.Equal(t, "4479748087", linkedInExpectedID(lazy.job.URL), "identity must come from the declared URL rather than the internal application ID")
}
