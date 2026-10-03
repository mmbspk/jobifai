package bot

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/documents"
	"github.com/user/jobifai/internal/domain"
)

func TestApplicationDocumentOverrides_ReusesCompletedResumeVersion(t *testing.T) {
	t.Parallel()
	l := &lazyDocGen{
		resumeVersionID: "resume-v1",
		coverVersionID:  "cover-v1",
	}
	o := l.applicationDocumentOverrides()
	require.Equal(t, "resume-v1", o.ResumeVersionID)
	require.Equal(t, "cover-v1", o.CoverVersionID)
}

func testBotWithProfile() *Bot {
	b := &Bot{}
	b.cfg.Profile = &domain.ResumeProfile{Summary: "Test profile"}
	b.cfg.Settings.DocumentPolicies = domain.DocumentPolicies{
		Version:            domain.DocumentPolicyMigrationVersion,
		ResumeMode:         domain.ResumeDocumentModeTailorJob,
		CoverMode:          domain.CoverDocumentModeWhenRequired,
		OnboardingComplete: true,
	}
	return b
}

func TestResolveWithCompletedResumeVersion_SkipsTailor(t *testing.T) {
	t.Parallel()
	b := testBotWithProfile()
	l := &lazyDocGen{
		b:               b,
		resumeVersionID: "already-tailored-v1",
		formCaps:        documents.FormDocumentCapabilities{Detected: true, ResumeFileSlots: 1, CoverRequired: true},
	}
	res := b.resolveApplicationDocs(l.applicationDocumentOverrides(), l.formCaps)
	require.False(t, res.Resume.NeedTailor)
	require.Equal(t, "already-tailored-v1", res.Resume.VersionID)
	require.True(t, res.Cover.NeedGenerateCover)
}

func TestAbsorbPartialMaterialization_KeepsResumeAfterCoverError(t *testing.T) {
	t.Parallel()
	l := &lazyDocGen{
		resume:          "/old/resume.pdf",
		resumeVersionID: "old-resume",
	}
	partial := documents.PreparedApplicationDocs{
		ResumePath:      "/new/resume.pdf",
		ResumeVersionID: "new-resume",
		Resume:          documents.ResolvedDocument{Kind: documents.KindResume, VersionID: "new-resume"},
	}
	l.absorbPartialMaterialization(partial, documents.ResolveResult{})
	require.Equal(t, "/new/resume.pdf", l.resume)
	require.Equal(t, "new-resume", l.resumeVersionID)
}

func TestLinkedInPrepareScanComplete_ReviewCTADoesNotComplete(t *testing.T) {
	t.Parallel()
	require.False(t, linkedInPrepareScanComplete("review", false))
	require.True(t, linkedInPrepareScanComplete("submit", false))
	require.True(t, linkedInPrepareScanComplete("", true))
}

type countingMaterializer struct {
	tailorCalls int
}

func (c *countingMaterializer) materialize(
	_ context.Context,
	l *lazyDocGen,
	res documents.ResolveResult,
) (documents.PreparedApplicationDocs, error) {
	if res.Resume.NeedTailor {
		c.tailorCalls++
		return documents.PreparedApplicationDocs{
			ResumePath:      "/tmp/resume.pdf",
			ResumeVersionID: "tailored-1",
			Resume:          res.Resume,
		}, nil
	}
	if res.Cover.NeedGenerateCover {
		return documents.PreparedApplicationDocs{}, errors.New("cover letter: quota")
	}
	return documents.PreparedApplicationDocs{}, nil
}

func TestMaterializeSequence_OneTailorAcrossResumeThenCoverCaps(t *testing.T) {
	t.Parallel()
	b := testBotWithProfile()
	l := &lazyDocGen{
		b:        b,
		ctx:      context.Background(),
		formCaps: documents.FormDocumentCapabilities{Detected: true, ResumeFileSlots: 1},
	}
	counter := &countingMaterializer{}

	res1 := b.resolveApplicationDocs(l.applicationDocumentOverrides(), l.formCaps)
	require.True(t, res1.Resume.NeedTailor)
	pack1, err := counter.materialize(context.Background(), l, res1)
	require.NoError(t, err)
	l.absorbPartialMaterialization(pack1, res1)
	l.materialized = false

	l.applyCaps(documents.FormDocumentCapabilities{Detected: true, ResumeFileSlots: 1, CoverRequired: true})
	res2 := b.resolveApplicationDocs(l.applicationDocumentOverrides(), l.formCaps)
	require.False(t, res2.Resume.NeedTailor)
	require.Equal(t, "tailored-1", res2.Resume.VersionID)
	_, err = counter.materialize(context.Background(), l, res2)
	require.Error(t, err)
	require.Equal(t, 1, counter.tailorCalls)
	require.Equal(t, "/tmp/resume.pdf", l.resume)
	require.Equal(t, "tailored-1", l.resumeVersionID)
}
