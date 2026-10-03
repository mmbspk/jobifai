package documents_test

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/documents"
)

func TestService_StoreOriginalUpload_ReferenceIntegrity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, db, userID := newTestService(t, &countingRenderer{})

	versionID, err := svc.StoreOriginalUpload(ctx, userID, "resume.pdf", "application/pdf", bytes.NewReader([]byte("raw-bytes")))
	require.NoError(t, err)

	detail, err := svc.GetVersion(ctx, userID, versionID)
	require.NoError(t, err)
	var ref documents.OriginalFileRef
	require.NoError(t, json.Unmarshal([]byte(detail.ContentJSON), &ref))
	require.NotEmpty(t, ref.OriginalFileID)

	var rowID string
	err = db.QueryRow(`SELECT id FROM document_original_files WHERE content_version_id = ?`, versionID).Scan(&rowID)
	require.NoError(t, err)
	require.Equal(t, ref.OriginalFileID, rowID)
}

func TestStore_InsertOriginalFile_UsesProvidedID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	store := documents.NewStore(db)
	userID := "u1"
	docID, err := store.CreateDocument(ctx, userID, documents.KindOriginalUpload, "f")
	require.NoError(t, err)
	vID, _, err := store.InsertVersion(ctx, documents.InsertVersionParams{
		DocumentID: docID, UserID: userID, Source: documents.SourceOriginalUpload,
		ContentKind: documents.ContentOriginalFileRef, ContentJSON: `{}`,
	})
	require.NoError(t, err)
	const want = "orig-fixed-id"
	require.NoError(t, store.InsertOriginalFile(ctx, userID, want, docID, vID, "f.pdf", "application/pdf", "k", "sha", 1))
	var got string
	require.NoError(t, db.QueryRow(`SELECT id FROM document_original_files WHERE id = ?`, want).Scan(&got))
	require.Equal(t, want, got)
}
