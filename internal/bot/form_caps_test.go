package bot

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/documents"
)

func TestCapsFromFormFields_StableDocumentKinds(t *testing.T) {
	t.Parallel()
	fields := []formField{
		{Type: "file", DocumentKind: documents.KindResume, Required: true},
		{Type: "file", DocumentKind: documents.KindCoverLetter, Required: false},
	}
	caps := capsFromFormFields(fields)
	require.True(t, caps.Detected)
	require.Equal(t, 1, caps.ResumeFileSlots)
	require.False(t, caps.CoverRequired)
	require.True(t, caps.CoverOptional)
}

func TestCapsFromFormFields_MultiStepCoverPage(t *testing.T) {
	t.Parallel()
	// Cover-only step (resume on a prior page) must not increment resume slots.
	coverStep := capsFromFormFields([]formField{
		{Type: "file", DocumentKind: documents.KindCoverLetter, Required: true, Question: "Cover letter"},
	})
	require.Equal(t, 0, coverStep.ResumeFileSlots)
	require.True(t, coverStep.CoverRequired)
}

func TestLazyApplyCaps_UnknownThenDetectedResetsMaterialization(t *testing.T) {
	t.Parallel()
	l := &lazyDocGen{
		formCaps: documents.FormDocumentCapabilities{Detected: false},
	}
	l.applyCaps(documents.FormDocumentCapabilities{Detected: false})
	l.holdReason = "form document requirements unknown — open apply form before preparing documents"
	l.materialized = true
	l.resume = "/tmp/old.pdf"

	l.applyCaps(documents.FormDocumentCapabilities{
		Detected: true, ResumeFileSlots: 1,
	})
	require.True(t, l.formCaps.Detected)
	require.False(t, l.materialized)
	require.Empty(t, l.resume)
}

func TestLazyApplyCaps_CoverStepPreservesResume(t *testing.T) {
	t.Parallel()
	l := &lazyDocGen{
		formCaps: documents.FormDocumentCapabilities{Detected: true, ResumeFileSlots: 1},
	}
	l.materialized = true
	l.resume = "/tmp/resume.pdf"
	l.resumeVersionID = "ver-resume"
	l.cover = ""

	l.applyCaps(documents.FormDocumentCapabilities{
		Detected: true, ResumeFileSlots: 1, CoverRequired: true,
	})
	require.False(t, l.materialized)
	require.Equal(t, "/tmp/resume.pdf", l.resume)
	require.Equal(t, "ver-resume", l.resumeVersionID)
	require.Empty(t, l.cover)
}
