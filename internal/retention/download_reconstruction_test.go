package retention_test

// Integration regression: submit → evict → change profile/template → download from history.
// Verifies: (1) saved content is accessible after eviction, (2) zero LLM calls
// (reconstruction uses stored profile snapshot, not the LLM), (3) the PDF is
// regenerated on demand and not written back to disk as a permanent export.

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/documents"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/resume"
	"github.com/user/jobifai/internal/retention"
)

type mockRenderer struct {
	calls atomic.Int32
}

func (r *mockRenderer) RenderResume(_ context.Context, _ *domain.ResumeProfile, _, _ string, _ *resume.RenderOptions) ([]byte, error) {
	r.calls.Add(1)
	return []byte("%PDF-resume-mock"), nil
}

func (r *mockRenderer) RenderCoverLetter(_ context.Context, _ string, _, _ string) ([]byte, error) {
	r.calls.Add(1)
	return []byte("%PDF-cover-mock"), nil
}

func TestDownloadAfterEviction_ReconstructsAndServesContent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// ── Setup ──────────────────────────────────────────────────────────────
	root := t.TempDir()
	dbPath := filepath.Join(root, "test.db")
	sqldb, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })

	blobRoot := filepath.Join(root, "blobs")
	blobs, err := documents.NewLocalBlobStore(blobRoot)
	require.NoError(t, err)

	renderer := &mockRenderer{}
	coord := &retention.ActivityCoordinator{}
	userID := "u-download-regression"

	profile := &domain.ResumeProfile{
		PersonalInformation: domain.PersonalInformation{Name: "Priya Test", Email: "priya@test.example"},
		Summary:             "Experienced designer with a decade of product work.",
	}
	docSvc := &documents.Service{
		Store:     documents.NewStore(sqldb),
		Blobs:     blobs,
		Renderer:  renderer,
		MarketDir: filepath.Join("..", "..", "resume_markets"),
		StylesDir: filepath.Join("..", "..", documents.StylesDirRelative),
		LoadProfile: func(uid string) (*domain.ResumeProfile, error) {
			return profile, nil
		},
		WorkGuard: coord,
	}

	// Use DocumentRetentionMin (5) — the lowest value Normalized() won't clamp.
	// With 5 filler applications above the target, the oldest is eligible for eviction.
	retSvc := &retention.Service{
		DB:       sqldb,
		Config:   &fakeConfigStore{limit: domain.DocumentRetentionMin},
		Root:     root,
		Blobs:    blobs,
		Activity: coord,
	}

	// ── Step 1: Create the target version and its export file ───────────────
	// This simulates "submit": profile is tailored, PDF rendered, path recorded.
	vOld, err := docSvc.CreateResumeFromProfile(ctx, userID, "Resume v1", documents.RenderContext{Language: "en"})
	require.NoError(t, err)
	require.Equal(t, int32(1), renderer.calls.Load(), "submit must call renderer once")

	oldExportRel := "job_applications/" + userID + "/resume-old-" + uuid.NewString() + ".pdf"
	oldExportAbs := filepath.Join(root, filepath.FromSlash(oldExportRel))
	require.NoError(t, os.MkdirAll(filepath.Dir(oldExportAbs), 0o755))
	require.NoError(t, os.WriteFile(oldExportAbs, []byte("%PDF-original-export"), 0o644))

	jobOldID := uuid.NewString()
	// Oldest timestamp — will be beyond the retention limit of 5.
	tsOld := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC).Format(time.RFC3339)
	_, err = sqldb.ExecContext(ctx, `
		INSERT INTO jobs_applied (id, user_id, platform, company, role, link,
			resume_path, resume_content_version_id, applied_at)
		VALUES (?, ?, 'seek', 'TargetCo', 'Designer', 'https://example.com/j0', ?, ?, ?)`,
		jobOldID, userID, oldExportRel, vOld, tsOld)
	require.NoError(t, err)

	// ── Step 2: Insert 5 filler applications (newer) to fill the retention window
	// These have no export paths — they just occupy the top-5 protected slots.
	for i := range domain.DocumentRetentionMin {
		ts := time.Date(2026, 1, 1, 10+i, 0, 0, 0, time.UTC).Format(time.RFC3339)
		_, err = sqldb.ExecContext(ctx, `
			INSERT INTO jobs_applied (id, user_id, platform, company, role, link, applied_at)
			VALUES (?, ?, 'seek', 'FillerCo', 'Role', 'https://example.com/f', ?)`,
			uuid.NewString(), userID, ts)
		require.NoError(t, err)
	}

	// ── Step 3: Run eviction — the oldest application's export should be cleared
	metrics, err := retSvc.RunEviction(ctx, userID, 10)
	require.NoError(t, err)
	require.GreaterOrEqual(t, metrics.PathsEvicted, 1, "expect at least one export path evicted")

	// The old export file must be gone from disk.
	_, statErr := os.Stat(oldExportAbs)
	require.True(t, os.IsNotExist(statErr), "evicted export file must not exist on disk")

	// The DB row must have resume_path cleared.
	var resumePath string
	require.NoError(t, sqldb.QueryRowContext(ctx,
		`SELECT COALESCE(resume_path,'') FROM jobs_applied WHERE id = ?`, jobOldID,
	).Scan(&resumePath))
	require.Empty(t, resumePath, "resume_path must be cleared in DB after eviction")

	// ── Step 4: Simulate profile/template change (zero extra LLM calls) ─────
	// The user updates their profile — the stored content version is unaffected.
	// A download of the OLD application must still reconstruct from the snapshot.
	profile.PersonalInformation.Name = "Priya Test (updated)"
	rendersBeforeDownload := renderer.calls.Load()

	// ── Step 5: Download from application history ────────────────────────────
	pdf, ctype, err := docSvc.PDFBytes(ctx, userID, vOld)
	require.NoError(t, err)
	require.Equal(t, "application/pdf", ctype)
	require.NotEmpty(t, pdf)

	rendersAfterDownload := renderer.calls.Load()
	// Exactly one render for reconstruction — no LLM tailor calls, just renderer.
	require.Equal(t, int32(1), rendersAfterDownload-rendersBeforeDownload,
		"download must trigger exactly one render (reconstruction), zero LLM calls")

	// ── Step 6: Reconstruction must NOT write a permanent export file ────────
	// PDFBytes returns bytes in memory; it does not persist a new blob or file.
	_, statErr = os.Stat(oldExportAbs)
	require.True(t, os.IsNotExist(statErr), "reconstructed PDF must not be re-persisted to disk")
}

// fakeConfigStore implements retention.ConfigStore with a hard-coded retention limit.
type fakeConfigStore struct {
	limit int
}

func (f *fakeConfigStore) Get(_, key string, dst any) error {
	if key == "document_retention_defaults" {
		if d, ok := dst.(*domain.DocumentRetentionDefaults); ok {
			*d = domain.DocumentRetentionDefaults{LatestSubmittedApplications: f.limit}
			return nil
		}
	}
	return domain.ErrNotFound
}

func (f *fakeConfigStore) Set(_, _ string, _ any) error { return nil }
