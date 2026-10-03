package documents_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/documents"
)

func testMetaStore(t *testing.T) (*documents.Service, string, func(documents.DefaultsMeta)) {
	t.Helper()
	svc, _, userID := newTestService(t, &countingRenderer{})
	var saved documents.DefaultsMeta
	svc.SaveDefaultsMeta = func(uid string, m documents.DefaultsMeta) error {
		require.Equal(t, userID, uid)
		saved = m
		return nil
	}
	svc.DefaultsMeta = func(uid string) (documents.DefaultsMeta, error) {
		return saved, nil
	}
	return svc, userID, func(m documents.DefaultsMeta) { saved = m }
}

func TestNotePreferredStyle_DefaultToNamedFlagsResumeOnly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, userID, setMeta := testMetaStore(t)

	vResume, err := svc.CreateResumeFromProfile(ctx, userID, "r", documents.RenderContext{Language: "en"})
	require.NoError(t, err)
	require.NoError(t, svc.SetDefault(ctx, userID, documents.KindResume, vResume))
	setMeta(documents.DefaultsMeta{ResumeStyle: ""})

	svc.NotePreferredStyle(userID, "us")
	meta, _ := svc.DefaultsMeta(userID)
	require.True(t, meta.ResumeOutdated)
	require.False(t, meta.CoverOutdated)
}

func TestNotePreferredStyle_BothDefaultsBothFlagsSetAtomically(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, userID, setMeta := testMetaStore(t)

	vResume, err := svc.CreateResumeFromProfile(ctx, userID, "r", documents.RenderContext{Language: "en"})
	require.NoError(t, err)
	require.NoError(t, svc.SetDefault(ctx, userID, documents.KindResume, vResume))

	vCover, err := svc.SaveCoverLetter(ctx, userID, "c", "body", documents.RenderContext{Language: "en"})
	require.NoError(t, err)
	require.NoError(t, svc.SetDefault(ctx, userID, documents.KindCoverLetter, vCover))
	setMeta(documents.DefaultsMeta{ResumeStyle: "", CoverStyle: "", ResumeOutdated: false, CoverOutdated: false})

	svc.NotePreferredStyle(userID, "us")
	meta, _ := svc.DefaultsMeta(userID)
	require.True(t, meta.ResumeOutdated, "resume default should be outdated when preferred style changes")
	require.True(t, meta.CoverOutdated, "cover default should be outdated when preferred style changes")
	require.NotEmpty(t, meta.OutdatedReason)
}

func TestSetDefaultCover_DoesNotOverwriteResumeStyle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, userID, _ := testMetaStore(t)

	vResume, err := svc.CreateResumeFromProfile(ctx, userID, "r", documents.RenderContext{Language: "en", StyleName: "us"})
	require.NoError(t, err)
	require.NoError(t, svc.SetDefault(ctx, userID, documents.KindResume, vResume))

	vCover, err := svc.SaveCoverLetter(ctx, userID, "c", "body", documents.RenderContext{Language: "en", StyleName: "uk"})
	require.NoError(t, err)
	require.NoError(t, svc.SetDefault(ctx, userID, documents.KindCoverLetter, vCover))

	meta, _ := svc.DefaultsMeta(userID)
	require.Equal(t, "us", meta.ResumeStyle)
	require.Equal(t, "uk", meta.CoverStyle)
}

func TestCreateAlternativeDocument_DoesNotMarkDefaultsOutdated(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, userID, setMeta := testMetaStore(t)

	v1, err := svc.CreateResumeFromProfile(ctx, userID, "r", documents.RenderContext{Language: "en"})
	require.NoError(t, err)
	require.NoError(t, svc.SetDefault(ctx, userID, documents.KindResume, v1))
	setMeta(documents.DefaultsMeta{ResumeOutdated: false, CoverOutdated: false, ResumeStyle: ""})

	_, err = svc.CreateResumeFromProfile(ctx, userID, "r2", documents.RenderContext{Language: "en", StyleName: "us"})
	require.NoError(t, err)

	meta, _ := svc.DefaultsMeta(userID)
	require.False(t, meta.ResumeOutdated)
	require.False(t, meta.CoverOutdated)
}
