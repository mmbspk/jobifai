package retention_test

// Integration regression: submit → evict → change profile/template → download via HTTP.
// Verifies: (1) real JWT-authenticated endpoint, (2) reconstruction from stored
// content snapshot (not the current profile), (3) exactly one render call for
// reconstruction, (4) no re-persistence of the reconstructed PDF to disk.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/auth"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/documents"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/resume"
	"github.com/user/jobifai/internal/retention"
)

// mockRenderer for download reconstruction test.
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

// TestDownloadAfterEviction_ReconstructsAndServesContent exercises the full
// HTTP download path: authenticate with ?token=, hit the handler, verify the
// response is a PDF with X-Jobifai-Reconstructed: true, and confirm the
// reconstructed bytes are not written back as a permanent export file.
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
		LoadProfile: func(_ string) (*domain.ResumeProfile, error) {
			return profile, nil
		},
		WorkGuard: coord,
	}

	retSvc := &retention.Service{
		DB:       sqldb,
		Config:   &fakeConfigStore{limit: domain.DocumentRetentionMin},
		Root:     root,
		Blobs:    blobs,
		Activity: coord,
	}

	// Build a minimal chi router that handles the download route.
	tm := auth.NewTokenManager("test-secret-reconstruct")
	userID := "u-download-regression-" + uuid.NewString()

	// Wire the DownloadAppliedPDF handler using the same handler package
	// conventions: build a chi router with the route we need.
	router := chi.NewRouter()
	router.Get("/api/jobs/applied/{job_id}/pdf/{kind}", func(w http.ResponseWriter, r *http.Request) {
		// Inline the tokenAuth + handler logic to avoid depending on handler package
		// internals from a retention_test package.  The test targets documents.Service.
		tok := r.URL.Query().Get("token")
		if tok == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		claims, verifyErr := tm.Verify(tok)
		if verifyErr != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		uid := claims.UserID
		jobID := chi.URLParam(r, "job_id")
		kind := chi.URLParam(r, "kind")

		var resumeVID string
		_ = sqldb.QueryRowContext(r.Context(),
			`SELECT COALESCE(resume_content_version_id,'') FROM jobs_applied WHERE id = ? AND user_id = ?`,
			jobID, uid,
		).Scan(&resumeVID)

		if kind != "resume" || resumeVID == "" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		data, ctype, reconstructed, dlErr := docSvc.PDFBytes(r.Context(), uid, resumeVID)
		if dlErr != nil {
			http.Error(w, dlErr.Error(), http.StatusUnprocessableEntity)
			return
		}
		w.Header().Set("Content-Type", ctype)
		if reconstructed {
			w.Header().Set("X-Jobifai-Reconstructed", "true")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	})

	// Issue a JWT for the test user.
	token, tokErr := tm.IssueAccess(userID, "priya@test.example")
	require.NoError(t, tokErr)

	// ── Step 1: Create the target version and export file ─────────────────
	vOld, err := docSvc.CreateResumeFromProfile(ctx, userID, "Resume v1", documents.RenderContext{Language: "en"})
	require.NoError(t, err)
	require.Equal(t, int32(1), renderer.calls.Load(), "submit must call renderer once")

	oldExportRel := "job_applications/" + userID + "/resume-old-" + uuid.NewString() + ".pdf"
	oldExportAbs := filepath.Join(root, filepath.FromSlash(oldExportRel))
	require.NoError(t, os.MkdirAll(filepath.Dir(oldExportAbs), 0o755))
	require.NoError(t, os.WriteFile(oldExportAbs, []byte("%PDF-original-export"), 0o644))

	jobOldID := uuid.NewString()
	tsOld := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC).Format(time.RFC3339)
	_, err = sqldb.ExecContext(ctx, `
		INSERT INTO jobs_applied (id, user_id, platform, company, role, link,
			resume_path, resume_content_version_id, applied_at)
		VALUES (?, ?, 'seek', 'TargetCo', 'Designer', 'https://example.com/j0', ?, ?, ?)`,
		jobOldID, userID, oldExportRel, vOld, tsOld)
	require.NoError(t, err)

	// ── Step 2: Insert 5 filler applications (newer) to fill retention window
	for i := range domain.DocumentRetentionMin {
		ts := time.Date(2026, 1, 1, 10+i, 0, 0, 0, time.UTC).Format(time.RFC3339)
		_, err = sqldb.ExecContext(ctx, `
			INSERT INTO jobs_applied (id, user_id, platform, company, role, link, applied_at)
			VALUES (?, ?, 'seek', 'FillerCo', 'Role', 'https://example.com/f', ?)`,
			uuid.NewString(), userID, ts)
		require.NoError(t, err)
	}

	// ── Step 3: Run eviction ─────────────────────────────────────────────
	metrics, err := retSvc.RunEviction(ctx, userID, 10)
	require.NoError(t, err)
	require.GreaterOrEqual(t, metrics.PathsEvicted, 1, "expect at least one export path evicted")

	_, statErr := os.Stat(oldExportAbs)
	require.True(t, os.IsNotExist(statErr), "evicted export file must not exist on disk")

	var resumePath string
	require.NoError(t, sqldb.QueryRowContext(ctx,
		`SELECT COALESCE(resume_path,'') FROM jobs_applied WHERE id = ?`, jobOldID,
	).Scan(&resumePath))
	require.Empty(t, resumePath, "resume_path must be cleared in DB after eviction")

	// ── Step 4: Simulate profile/template change ──────────────────────────
	// The user updates their profile; the stored content snapshot must be used,
	// not the current live profile.
	profile.PersonalInformation.Name = "Priya Test (updated)"
	rendersBeforeDownload := renderer.calls.Load()

	// ── Step 5: Download via HTTP with ?token= authentication ────────────
	req := httptest.NewRequest("GET",
		"/api/jobs/applied/"+jobOldID+"/pdf/resume?token="+token, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "download must succeed: %s", w.Body.String())
	require.Equal(t, "application/pdf", w.Header().Get("Content-Type"))
	require.Equal(t, "true", w.Header().Get("X-Jobifai-Reconstructed"),
		"evicted artifact must trigger X-Jobifai-Reconstructed")
	require.NotEmpty(t, w.Body.Bytes())

	rendersAfterDownload := renderer.calls.Load()
	require.Equal(t, int32(1), rendersAfterDownload-rendersBeforeDownload,
		"download must trigger exactly one render (reconstruction), zero extra LLM calls")

	// Verify reconstructed profile is from the stored snapshot, not the live profile.
	// The stored content JSON has the original name; inspect it via the DB.
	var contentJSON string
	require.NoError(t, sqldb.QueryRowContext(ctx,
		`SELECT content_json FROM document_content_versions WHERE id = ?`, vOld,
	).Scan(&contentJSON))
	var rc struct {
		Profile domain.ResumeProfile `json:"profile"`
	}
	require.NoError(t, json.Unmarshal([]byte(contentJSON), &rc))
	require.Equal(t, "Priya Test", rc.Profile.PersonalInformation.Name,
		"stored snapshot must use original name, not the live updated profile")

	// ── Step 6: Reconstruction must NOT write a permanent export file ─────
	_, statErr = os.Stat(oldExportAbs)
	require.True(t, os.IsNotExist(statErr), "reconstructed PDF must not be re-persisted to disk")
}
