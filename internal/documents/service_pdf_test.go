package documents_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/documents"
)

func TestService_PDFBytes_ReconstructWhenBlobMissing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cr := &countingRenderer{}
	svc, _, userID := newTestService(t, cr)

	versionID, err := svc.CreateResumeFromProfile(ctx, userID, "", documents.RenderContext{Language: "en"})
	require.NoError(t, err)

	key, _, err := svc.Store.ArtifactForVersion(ctx, userID, versionID)
	require.NoError(t, err)

	abs := filepath.Join(svc.Blobs.(*documents.LocalBlobStore).Root, filepath.FromSlash(key))
	require.NoError(t, os.Remove(abs))

	cr.calls.Store(0)
	pdf, _, err := svc.PDFBytes(ctx, userID, versionID)
	require.NoError(t, err)
	require.NotEmpty(t, pdf)
	require.Equal(t, int32(1), cr.calls.Load())
}
