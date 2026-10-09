package documents_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

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

func TestStore_ListDocuments_ConcurrentDoesNotDeadlock(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(4)
	store := documents.NewStore(db)
	userID := "u-concurrent"
	for i := 0; i < 6; i++ {
		docID, err := store.CreateDocument(ctx, userID, documents.KindResume, "doc")
		require.NoError(t, err)
		_, _, err = store.InsertVersion(ctx, documents.InsertVersionParams{
			DocumentID: docID, UserID: userID, Source: documents.SourceProfileRender,
			ContentKind: documents.ContentResumeJSON, ContentJSON: `{}`,
			RendererVersion: documents.RendererVersion, Reconstructible: false,
		})
		require.NoError(t, err)
	}

	errCh := make(chan error, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := store.ListDocuments(ctx, userID)
			errCh <- err
		}()
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("ListDocuments concurrent calls deadlocked")
	}
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}
}
