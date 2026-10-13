package documents_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/documents"
)

func TestDeleteDocument_PreservesSharedArtifactsAndOtherDefault(t *testing.T) {
	ctx := context.Background()
	svc, db, userID := newTestService(t, &countingRenderer{})
	resumeID, err := svc.CreateResumeFromProfile(ctx, userID, "Resume", documents.RenderContext{})
	require.NoError(t, err)
	coverID, err := svc.SaveCoverLetter(ctx, userID, "Cover", "A cover letter", documents.RenderContext{})
	require.NoError(t, err)
	otherResumeID, err := svc.CreateResumeFromProfile(ctx, userID, "Another resume", documents.RenderContext{})
	require.NoError(t, err)
	require.NoError(t, svc.SetDefault(ctx, userID, documents.KindResume, resumeID))
	require.NoError(t, svc.SetDefault(ctx, userID, documents.KindCoverLetter, coverID))
	detail, err := svc.GetVersion(ctx, userID, resumeID)
	require.NoError(t, err)
	require.NoError(t, svc.DeleteDocument(ctx, userID, detail.DocumentID))
	list, err := svc.List(ctx, userID)
	require.NoError(t, err)
	require.Len(t, list.Documents, 2)
	require.Empty(t, list.Defaults.ResumeVersionID)
	require.Equal(t, coverID, list.Defaults.CoverLetterVersionID)
	data, _, _, err := svc.PDFBytes(ctx, userID, otherResumeID)
	require.NoError(t, err)
	require.NotEmpty(t, data, "a shared PDF must remain downloadable")
	var artifacts int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM document_render_artifacts`).Scan(&artifacts))
	require.Equal(t, 2, artifacts, "identical resume PDFs share an artifact; cover remains independent")
	// Direct inserts and defaults must also reject a deleted editing target.
	_, _, err = svc.Store.InsertVersion(ctx, documents.InsertVersionParams{
		DocumentID: detail.DocumentID, UserID: userID, Source: documents.SourceUserEdit,
		ContentKind: documents.ContentResumeJSON, ContentJSON: `{}`,
	})
	require.ErrorIs(t, err, documents.ErrNotFound)
}

func TestDeleteDocument_DefaultFailureRollsBackLibraryRemoval(t *testing.T) {
	ctx := context.Background()
	svc, db, userID := newTestService(t, &countingRenderer{})
	versionID, err := svc.CreateResumeFromProfile(ctx, userID, "Default resume", documents.RenderContext{})
	require.NoError(t, err)
	require.NoError(t, svc.SetDefault(ctx, userID, documents.KindResume, versionID))
	detail, err := svc.GetVersion(ctx, userID, versionID)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TRIGGER deny_default_delete BEFORE DELETE ON user_document_defaults BEGIN SELECT RAISE(ABORT, 'test failure'); END`)
	require.NoError(t, err)
	require.Error(t, svc.DeleteDocument(ctx, userID, detail.DocumentID))
	list, err := svc.List(ctx, userID)
	require.NoError(t, err)
	require.Len(t, list.Documents, 1)
	require.Equal(t, versionID, list.Defaults.ResumeVersionID)
}

type canceledDeleteGuard struct{}

func (canceledDeleteGuard) BeginWork(context.Context, string) error { return context.Canceled }
func (canceledDeleteGuard) EndWork(string)                          {}

func TestDeleteDocument_GuardFailureLeavesDocumentActive(t *testing.T) {
	ctx := context.Background()
	svc, _, userID := newTestService(t, &countingRenderer{})
	versionID, err := svc.CreateResumeFromProfile(ctx, userID, "Resume", documents.RenderContext{})
	require.NoError(t, err)
	detail, err := svc.GetVersion(ctx, userID, versionID)
	require.NoError(t, err)
	svc.WorkGuard = canceledDeleteGuard{}
	require.ErrorIs(t, svc.DeleteDocument(ctx, userID, detail.DocumentID), context.Canceled)
	list, err := svc.List(ctx, userID)
	require.NoError(t, err)
	require.Len(t, list.Documents, 1)
}
