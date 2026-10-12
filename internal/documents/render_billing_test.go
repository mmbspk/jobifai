package documents_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/documents"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/quota"
	"github.com/user/jobifai/internal/resume"
)

// stubBiller records calls to BeforeRender and RecordRender for billing assertions.
type stubBiller struct {
	beforeErr  error // returned by BeforeRender when set
	recordErr  error // returned by RecordRender when set
	beforeN    atomic.Int32
	recordN    atomic.Int32
	recordKeys []string // protected by recordN ordering (single-goroutine tests)
}

func (b *stubBiller) BeforeRender(_ context.Context, _ string) error {
	b.beforeN.Add(1)
	return b.beforeErr
}

func (b *stubBiller) RecordRender(_ context.Context, _, idempotencyKey string) error {
	b.recordN.Add(1)
	b.recordKeys = append(b.recordKeys, idempotencyKey)
	return b.recordErr
}

// failingRenderer always fails.
type failingRenderer struct{}

func (f *failingRenderer) RenderResume(_ context.Context, _ *domain.ResumeProfile, _, _ string, _ *resume.RenderOptions) ([]byte, error) {
	return nil, errors.New("chrome unavailable")
}

func (f *failingRenderer) RenderCoverLetter(_ context.Context, _, _, _ string) ([]byte, error) {
	return nil, errors.New("chrome unavailable")
}

// newBilledService creates a Service with a stubBiller wired in.
func newBilledService(t *testing.T, r documents.PDFRenderer, b *stubBiller) (*documents.Service, string) {
	t.Helper()
	svc, _, userID := newTestService(t, r)
	svc.Biller = b
	return svc, userID
}

// TestRenderBilling_NewResume_ExactlyOneBurn verifies a single render event is recorded
// when creating a new resume version.
func TestRenderBilling_NewResume_ExactlyOneBurn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b := &stubBiller{}
	svc, userID := newBilledService(t, &countingRenderer{}, b)

	versionID, err := svc.CreateResumeFromProfile(ctx, userID, "", documents.RenderContext{Language: "en"})
	require.NoError(t, err)
	require.NotEmpty(t, versionID)

	assert.Equal(t, int32(1), b.beforeN.Load(), "BeforeRender must be called exactly once")
	assert.Equal(t, int32(1), b.recordN.Load(), "RecordRender must be called exactly once")
	assert.Equal(t, []string{"document-render:" + versionID}, b.recordKeys,
		"idempotency key must be document-render:<versionID>")
}

// TestRenderBilling_AppendVersion_NewBurnPerVersion verifies each new version incurs exactly
// one additional render burn.
func TestRenderBilling_AppendVersion_NewBurnPerVersion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b := &stubBiller{}
	svc, userID := newBilledService(t, &countingRenderer{}, b)

	v1, err := svc.CreateResumeFromProfile(ctx, userID, "First", documents.RenderContext{Language: "en"})
	require.NoError(t, err)

	// Retrieve the document ID so we can append to the same document.
	list, err := svc.List(ctx, userID)
	require.NoError(t, err)
	require.Len(t, list.Documents, 1)
	docID := list.Documents[0].ID
	profile := &domain.ResumeProfile{
		PersonalInformation: domain.PersonalInformation{Name: "Alex Example", Email: "alex@example.com"},
	}

	v2, err := svc.AppendResumeVersion(ctx, userID, docID, "Second", documents.SourceUserEdit, profile, documents.RenderContext{Language: "en"})
	require.NoError(t, err)

	assert.Equal(t, int32(2), b.recordN.Load(), "two distinct versions → two render burns")
	assert.NotEqual(t, v1, v2, "version IDs must differ")
	assert.Contains(t, b.recordKeys, "document-render:"+v1)
	assert.Contains(t, b.recordKeys, "document-render:"+v2)
}

// TestRenderBilling_CoverLetter_ExactlyOneBurn verifies a cover letter save incurs one burn.
func TestRenderBilling_CoverLetter_ExactlyOneBurn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b := &stubBiller{}
	svc, userID := newBilledService(t, &countingRenderer{}, b)

	versionID, err := svc.SaveCoverLetter(ctx, userID, "My Cover", "Dear Hiring Manager,\nI am keen.", documents.RenderContext{Language: "en"})
	require.NoError(t, err)
	require.NotEmpty(t, versionID)

	assert.Equal(t, int32(1), b.beforeN.Load(), "BeforeRender must fire once for cover letter")
	assert.Equal(t, int32(1), b.recordN.Load(), "RecordRender must fire once for cover letter")
	assert.Equal(t, []string{"document-render:" + versionID}, b.recordKeys)
}

// TestRenderBilling_InsufficientCredits_RendererNeverCalled verifies that when BeforeRender
// returns quota.ErrExceeded, the PDF renderer is never invoked.
func TestRenderBilling_InsufficientCredits_RendererNeverCalled(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cr := &countingRenderer{}
	b := &stubBiller{beforeErr: fmt.Errorf("preflight: %w", quota.ErrExceeded)}
	svc, userID := newBilledService(t, cr, b)

	_, err := svc.CreateResumeFromProfile(ctx, userID, "", documents.RenderContext{Language: "en"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, quota.ErrExceeded), "error must wrap quota.ErrExceeded")
	assert.Equal(t, int32(0), cr.calls.Load(), "renderer must not be called when preflight fails")
	assert.Equal(t, int32(0), b.recordN.Load(), "RecordRender must not be called when preflight fails")
}

// TestRenderBilling_FailedRender_NoBurn verifies that a renderer failure does not produce
// a billing event.
func TestRenderBilling_FailedRender_NoBurn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b := &stubBiller{}
	svc, userID := newBilledService(t, &failingRenderer{}, b)

	_, err := svc.CreateResumeFromProfile(ctx, userID, "", documents.RenderContext{Language: "en"})
	require.Error(t, err)

	assert.Equal(t, int32(1), b.beforeN.Load(), "BeforeRender must have been called (preflight passed)")
	assert.Equal(t, int32(0), b.recordN.Load(), "RecordRender must NOT fire when the renderer failed")
}

// TestRenderBilling_AccountingFailure_NoSuccessfulResponse verifies that when RecordRender
// returns an error (accounting persistence failure), the entire save operation fails —
// the caller receives no success for an unaccounted render.
func TestRenderBilling_AccountingFailure_NoSuccessfulResponse(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cr := &countingRenderer{}
	b := &stubBiller{recordErr: errors.New("db write timeout")}
	svc, userID := newBilledService(t, cr, b)

	_, err := svc.CreateResumeFromProfile(ctx, userID, "", documents.RenderContext{Language: "en"})
	require.Error(t, err, "accounting failure must propagate as an error — no silent success")
	assert.Equal(t, int32(1), cr.calls.Load(), "renderer ran before accounting failed")
	assert.Equal(t, int32(1), b.recordN.Load(), "RecordRender was attempted")
}

// TestRenderBilling_NilBiller_NoError verifies that when no Biller is configured (dev/test
// deployments), all render paths succeed without attempting billing.
func TestRenderBilling_NilBiller_NoError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cr := &countingRenderer{}
	svc, _, userID := newTestService(t, cr)
	// svc.Biller is nil by default in newTestService.

	versionID, err := svc.CreateResumeFromProfile(ctx, userID, "", documents.RenderContext{Language: "en"})
	require.NoError(t, err)
	require.NotEmpty(t, versionID)
	assert.Equal(t, int32(1), cr.calls.Load(), "renderer must be called normally when billing is disabled")
}

// TestRenderBilling_PDFBytes_ZeroCredits verifies that serving an existing artifact or
// reconstructing an evicted one never calls the biller.
func TestRenderBilling_PDFBytes_ZeroCredits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cr := &countingRenderer{}
	b := &stubBiller{}
	svc, userID := newBilledService(t, cr, b)

	// 1. Create a version so there is an artifact on disk.
	versionID, err := svc.CreateResumeFromProfile(ctx, userID, "", documents.RenderContext{Language: "en"})
	require.NoError(t, err)

	// Reset billing counters after the initial render.
	b.beforeN.Store(0)
	b.recordN.Store(0)

	// 2. Serve from stored artifact — must be zero extra billing calls.
	pdf, _, reconstructed, err := svc.PDFBytes(ctx, userID, versionID)
	require.NoError(t, err)
	require.NotEmpty(t, pdf)
	assert.False(t, reconstructed, "first PDFBytes call should serve from the stored artifact")
	assert.Equal(t, int32(0), b.beforeN.Load(), "BeforeRender must not fire on download")
	assert.Equal(t, int32(0), b.recordN.Load(), "RecordRender must not fire on download")

	// 3. Repeated download — still zero.
	_, _, reconstructed2, err := svc.PDFBytes(ctx, userID, versionID)
	require.NoError(t, err)
	assert.False(t, reconstructed2)
	assert.Equal(t, int32(0), b.recordN.Load(), "repeated download must not bill")
}

// TestRenderBilling_Reconstruction_ZeroCredits verifies that on-demand reconstruction of
// an evicted artifact is never billed.
func TestRenderBilling_Reconstruction_ZeroCredits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cr := &countingRenderer{}
	b := &stubBiller{}
	svc, userID := newBilledService(t, cr, b)

	versionID, err := svc.CreateResumeFromProfile(ctx, userID, "", documents.RenderContext{Language: "en"})
	require.NoError(t, err)

	// Evict the artifact by deleting it from blob storage.
	key, _, err := svc.Store.ArtifactForVersion(ctx, userID, versionID)
	require.NoError(t, err)
	abs := filepath.Join(svc.Blobs.(*documents.LocalBlobStore).Root, filepath.FromSlash(key))
	require.NoError(t, os.Remove(abs))

	// Reset counters after initial render.
	b.beforeN.Store(0)
	b.recordN.Store(0)
	cr.calls.Store(0)

	// Reconstruction via PDFBytes must succeed without billing.
	pdf, _, reconstructed, err := svc.PDFBytes(ctx, userID, versionID)
	require.NoError(t, err)
	require.NotEmpty(t, pdf)
	assert.True(t, reconstructed, "PDFBytes must report reconstruction when blob is missing")
	assert.Equal(t, int32(0), b.beforeN.Load(), "BeforeRender must not fire during reconstruction")
	assert.Equal(t, int32(0), b.recordN.Load(), "RecordRender must not fire during reconstruction")
	assert.Equal(t, int32(1), cr.calls.Load(), "renderer must be invoked during reconstruction")
}

// TestRenderBilling_IdempotencyKey_Format verifies the idempotency key format is
// "document-render:<versionID>" for versioned paths.
func TestRenderBilling_IdempotencyKey_Format(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b := &stubBiller{}
	svc, userID := newBilledService(t, &countingRenderer{}, b)

	versionID, err := svc.CreateResumeFromProfile(ctx, userID, "", documents.RenderContext{Language: "en"})
	require.NoError(t, err)

	require.Len(t, b.recordKeys, 1)
	assert.Equal(t, "document-render:"+versionID, b.recordKeys[0],
		"idempotency key must be document-render:<versionID> for versioned paths")
}

// TestRenderBilling_CoverLetter_AccountingFailure verifies the same accounting-failure
// semantics apply to cover letter renders.
func TestRenderBilling_CoverLetter_AccountingFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b := &stubBiller{recordErr: errors.New("timeout")}
	svc, userID := newBilledService(t, &countingRenderer{}, b)

	_, err := svc.SaveCoverLetter(ctx, userID, "", "Cover body text.", documents.RenderContext{Language: "en"})
	require.Error(t, err, "accounting failure for cover render must propagate as error")
}

// TestRenderBilling_CoverLetter_InsufficientCredits verifies that quota exhaustion prevents
// the cover letter renderer from being called.
func TestRenderBilling_CoverLetter_InsufficientCredits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cr := &countingRenderer{}
	b := &stubBiller{beforeErr: fmt.Errorf("quota: %w", quota.ErrExceeded)}
	svc, userID := newBilledService(t, cr, b)

	_, err := svc.SaveCoverLetter(ctx, userID, "", "Cover body text.", documents.RenderContext{Language: "en"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, quota.ErrExceeded))
	assert.Equal(t, int32(0), cr.calls.Load(), "renderer must not be called when preflight fails")
}

// TestRenderBilling_ReconstructPDF_ZeroCredits verifies that calling ReconstructPDF directly
// (as opposed to via PDFBytes) also produces no billing events.
func TestRenderBilling_ReconstructPDF_ZeroCredits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b := &stubBiller{}
	svc, userID := newBilledService(t, &countingRenderer{}, b)

	versionID, err := svc.CreateResumeFromProfile(ctx, userID, "", documents.RenderContext{Language: "en"})
	require.NoError(t, err)

	// Reset counters.
	b.beforeN.Store(0)
	b.recordN.Store(0)

	pdf, _, err := svc.ReconstructPDF(ctx, userID, versionID)
	require.NoError(t, err)
	require.NotEmpty(t, pdf)
	assert.Equal(t, int32(0), b.beforeN.Load(), "ReconstructPDF must not check quota")
	assert.Equal(t, int32(0), b.recordN.Load(), "ReconstructPDF must not bill")
}
