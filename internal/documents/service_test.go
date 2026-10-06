package documents_test

import (
	"bytes"
	"context"
	"database/sql"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/documents"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/resume"
)

type countingRenderer struct {
	calls    atomic.Int32
	lastName atomic.Value
}

func (c *countingRenderer) RenderResume(ctx context.Context, profile *domain.ResumeProfile, styleName, cssOverride string, opts *resume.RenderOptions) ([]byte, error) {
	c.calls.Add(1)
	if profile != nil {
		c.lastName.Store(profile.PersonalInformation.Name)
	}
	return []byte("%PDF-resume"), nil
}

func (c *countingRenderer) RenderCoverLetter(ctx context.Context, body string, styleName, cssOverride string) ([]byte, error) {
	c.calls.Add(1)
	return []byte("%PDF-cover"), nil
}

func newTestService(t *testing.T, r documents.PDFRenderer) (*documents.Service, *sql.DB, string) {
	t.Helper()
	db, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	root := filepath.Join(t.TempDir(), "blobs")
	blobs, err := documents.NewLocalBlobStore(root)
	require.NoError(t, err)
	userID := "user-a"
	svc := &documents.Service{
		Store:     documents.NewStore(db),
		Blobs:     blobs,
		Renderer:  r,
		MarketDir: filepath.Join("..", "..", "resume_markets"),
		StylesDir: filepath.Join("..", "..", documents.StylesDirRelative),
		LoadProfile: func(uid string) (*domain.ResumeProfile, error) {
			if uid != userID {
				return nil, nil
			}
			return &domain.ResumeProfile{
				PersonalInformation: domain.PersonalInformation{
					Name:  "Alex Example",
					Email: "alex@example.com",
				},
			}, nil
		},
	}
	return svc, db, userID
}

func TestService_CreateResumeFromProfile_OwnershipAndPDF(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cr := &countingRenderer{}
	svc, _, userID := newTestService(t, cr)

	versionID, err := svc.CreateResumeFromProfile(ctx, userID, "My resume", documents.RenderContext{Language: "en"})
	require.NoError(t, err)
	require.NotEmpty(t, versionID)

	_, _, _, err = svc.PDFBytes(ctx, "other-user", versionID)
	require.ErrorIs(t, err, documents.ErrForbidden)

	pdf, _, _, err := svc.PDFBytes(ctx, userID, versionID)
	require.NoError(t, err)
	assert.True(t, bytes.HasPrefix(pdf, []byte("%PDF")))
}

func TestService_ReconstructPDF_ZeroRendererCallsWhenArtifactExists(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cr := &countingRenderer{}
	svc, _, userID := newTestService(t, cr)

	versionID, err := svc.CreateResumeFromProfile(ctx, userID, "", documents.RenderContext{Language: "en"})
	require.NoError(t, err)
	require.Equal(t, int32(1), cr.calls.Load())

	_, _, _, err = svc.PDFBytes(ctx, userID, versionID)
	require.NoError(t, err)
	assert.Equal(t, int32(1), cr.calls.Load(), "download must not re-render when artifact exists")
}

func TestService_ReconstructPDF_UsesSavedContentNotLiveProfile(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cr := &countingRenderer{}
	svc, db, userID := newTestService(t, cr)

	versionID, err := svc.CreateResumeFromProfile(ctx, userID, "", documents.RenderContext{Language: "en"})
	require.NoError(t, err)

	_, err = db.Exec(`DELETE FROM document_version_artifact_refs WHERE content_version_id = ?`, versionID)
	require.NoError(t, err)
	_, err = db.Exec(`DELETE FROM document_render_artifacts WHERE content_version_id = ?`, versionID)
	require.NoError(t, err)

	// Simulate profile change after version was saved.
	svc.LoadProfile = func(uid string) (*domain.ResumeProfile, error) {
		return &domain.ResumeProfile{PersonalInformation: domain.PersonalInformation{Name: "Changed Name"}}, nil
	}

	cr.calls.Store(0)
	pdf, _, err := svc.ReconstructPDF(ctx, userID, versionID)
	require.NoError(t, err)
	assert.True(t, bytes.HasPrefix(pdf, []byte("%PDF")))
	assert.Equal(t, int32(1), cr.calls.Load())
	assert.Equal(t, "Alex Example", cr.lastName.Load())
}

func TestService_SetDefault_ExplicitOnly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _, userID := newTestService(t, &countingRenderer{})

	v1, err := svc.CreateResumeFromProfile(ctx, userID, "v1", documents.RenderContext{Language: "en"})
	require.NoError(t, err)
	v2, err := svc.CreateResumeFromProfile(ctx, userID, "v2", documents.RenderContext{Language: "en"})
	require.NoError(t, err)

	list, err := svc.List(ctx, userID)
	require.NoError(t, err)
	assert.Empty(t, list.Defaults.ResumeVersionID)

	require.NoError(t, svc.SetDefault(ctx, userID, documents.KindResume, v1))
	list, err = svc.List(ctx, userID)
	require.NoError(t, err)
	assert.Equal(t, v1, list.Defaults.ResumeVersionID)

	require.NoError(t, svc.SetDefault(ctx, userID, documents.KindResume, v2))
	list, err = svc.List(ctx, userID)
	require.NoError(t, err)
	assert.Equal(t, v2, list.Defaults.ResumeVersionID)
}

func TestService_OriginalUpload_NonReconstructible(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _, userID := newTestService(t, &countingRenderer{})

	vID, err := svc.StoreOriginalUpload(ctx, userID, "resume.pdf", "application/pdf", bytes.NewReader([]byte("raw-bytes")))
	require.NoError(t, err)

	_, _, p, err := svc.Store.GetVersionRow(ctx, userID, vID)
	require.NoError(t, err)
	assert.False(t, p.Reconstructible)

	_, _, err = svc.ReconstructPDF(ctx, userID, vID)
	require.Error(t, err)
}
