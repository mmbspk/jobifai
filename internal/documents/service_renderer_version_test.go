package documents_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/documents"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/resume"
)

type versionRecordingRenderer struct {
	countingRenderer
	version string
}

func (r *versionRecordingRenderer) RenderResume(ctx context.Context, profile *domain.ResumeProfile, style, css string, opts *resume.RenderOptions) ([]byte, error) {
	r.version = opts.RendererVersion
	return r.countingRenderer.RenderResume(ctx, profile, style, css, opts)
}

func TestPDFBytes_ReconstructionUsesLegacySnapshotRenderer(t *testing.T) {
	ctx := context.Background()
	renderer := &versionRecordingRenderer{}
	svc, db, userID := newTestService(t, renderer)
	versionID, err := svc.CreateResumeFromProfile(ctx, userID, "Resume", documents.RenderContext{})
	require.NoError(t, err)
	require.Equal(t, resume.PDFRendererVersion, renderer.version)
	_, _, saved, err := svc.Store.GetVersionRow(ctx, userID, versionID)
	require.NoError(t, err)
	snapshot, err := documents.ParseRenderSnapshot(saved.RenderSnapshotJSON)
	require.NoError(t, err)
	// Model an existing v1 row, including its frozen section labels and CSS.
	snapshot.RendererVersion = resume.LegacyPDFRendererVersion
	raw, err := json.Marshal(snapshot)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE document_content_versions SET renderer_version=?, render_snapshot_json=? WHERE id=?`, resume.LegacyPDFRendererVersion, string(raw), versionID)
	require.NoError(t, err)
	key, _, err := svc.Store.ArtifactForVersion(ctx, userID, versionID)
	require.NoError(t, err)
	require.NoError(t, svc.Blobs.Remove(key))
	_, _, rebuilt, err := svc.PDFBytes(ctx, userID, versionID)
	require.NoError(t, err)
	require.True(t, rebuilt)
	require.Equal(t, resume.LegacyPDFRendererVersion, renderer.version)
}
