package documents_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/documents"
)

func TestStore_ListDocuments_EmptyAndOriginalUpload(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	store := documents.NewStore(db)
	userID := "u1"

	docs, _, err := store.ListDocuments(ctx, userID)
	require.NoError(t, err)
	require.NotNil(t, docs)
	require.Len(t, docs, 0)

	docID, err := store.CreateDocument(ctx, userID, documents.KindOriginalUpload, "cv.pdf")
	require.NoError(t, err)
	_, _, err = store.InsertVersion(ctx, documents.InsertVersionParams{
		DocumentID: docID, UserID: userID, Source: documents.SourceOriginalUpload,
		ContentKind: documents.ContentOriginalFileRef, ContentJSON: `{"filename":"cv.pdf"}`,
		RendererVersion: documents.RendererVersion, Reconstructible: false,
	})
	require.NoError(t, err)

	docs, _, err = store.ListDocuments(ctx, userID)
	require.NoError(t, err)
	require.Len(t, docs, 1)
	require.Len(t, docs[0].Versions, 1)
	require.False(t, docs[0].Versions[0].HasPDF)
}
