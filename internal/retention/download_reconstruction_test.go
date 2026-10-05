package retention_test

// Integration regression: submit → evict → change profile/template → download via
// production HTTP handler.
//
// Verifies:
//   (1) production handler.NewRouter route handles ?token= JWT auth
//   (2) reconstruction uses the stored content/CSS snapshot (not the live profile)
//   (3) exactly one render call, and the profile/CSS passed to the renderer
//       matches the snapshot — not the updated live inputs
//   (4) artifact blob is NOT written to blob storage after reconstruction
//   (5) reuse and reconstruction metrics are incremented correctly

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/auth"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/documents"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/handler"
	"github.com/user/jobifai/internal/resume"
	"github.com/user/jobifai/internal/retention"
)

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

// capturingRenderer records what was actually passed to RenderResume so the
// test can verify reconstruction used the stored snapshot, not live inputs.
type capturingRenderer struct {
	mu          sync.Mutex
	resumeCalls []capturedRenderCall
	count       atomic.Int32
}

type capturedRenderCall struct {
	profileName string // profile.PersonalInformation.Name
	cssContent  string // content of the cssOverride temp-file
}

func (r *capturingRenderer) RenderResume(_ context.Context, profile *domain.ResumeProfile, _, cssOverride string, _ *resume.RenderOptions) ([]byte, error) {
	r.count.Add(1)
	var cssContent string
	if data, err := os.ReadFile(cssOverride); err == nil {
		cssContent = string(data)
	}
	r.mu.Lock()
	r.resumeCalls = append(r.resumeCalls, capturedRenderCall{
		profileName: profile.PersonalInformation.Name,
		cssContent:  cssContent,
	})
	r.mu.Unlock()
	return []byte("%PDF-capture-" + profile.PersonalInformation.Name), nil
}

func (r *capturingRenderer) RenderCoverLetter(_ context.Context, _ string, _, _ string) ([]byte, error) {
	r.count.Add(1)
	return []byte("%PDF-cover-capture"), nil
}

func (r *capturingRenderer) lastCall() (capturedRenderCall, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.resumeCalls) == 0 {
		return capturedRenderCall{}, false
	}
	return r.resumeCalls[len(r.resumeCalls)-1], true
}

// countBlobFiles returns the number of files recursively under root.
func countBlobFiles(root string) (int, error) {
	var n int
	err := filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			n++
		}
		return nil
	})
	return n, err
}

// registerUser calls /auth/register on the router and returns the access token.
func registerUser(t *testing.T, router http.Handler, email, password string) (accessToken, userID string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"email": email, "password": password})
	req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code, "register: %s", w.Body)
	var tok auth.Tokens
	require.NoError(t, json.NewDecoder(w.Body).Decode(&tok))

	// Get user ID via /api/me
	meReq := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	mew := httptest.NewRecorder()
	router.ServeHTTP(mew, meReq)
	require.Equal(t, http.StatusOK, mew.Code)
	var me map[string]any
	require.NoError(t, json.NewDecoder(mew.Body).Decode(&me))
	return tok.AccessToken, me["id"].(string)
}

// TestDownloadAfterEviction_ReconstructsAndServesContent exercises the full
// submit → evict → download flow via the production HTTP router.
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

	capturer := &capturingRenderer{}
	coord := &retention.ActivityCoordinator{}
	docMetrics := &documents.ServiceMetrics{}

	// profile is the live profile pointer; the test mutates it after version creation
	// to simulate a profile/template update that happens after submission.
	profile := &domain.ResumeProfile{
		PersonalInformation: domain.PersonalInformation{
			Name:  "Priya Snapshot",
			Email: "priya@example.test",
		},
		Summary: "Experienced designer with a decade of product work.",
	}
	docSvc := &documents.Service{
		Store:     documents.NewStore(sqldb),
		Blobs:     blobs,
		Renderer:  capturer,
		MarketDir: filepath.Join("..", "..", "resume_markets"),
		StylesDir: filepath.Join("..", "..", documents.StylesDirRelative),
		LoadProfile: func(_ string) (*domain.ResumeProfile, error) {
			return profile, nil
		},
		WorkGuard: coord,
		Metrics:   docMetrics,
	}

	retSvc := &retention.Service{
		DB:       sqldb,
		Config:   &fakeConfigStore{limit: domain.DocumentRetentionMin},
		Root:     root,
		Blobs:    blobs,
		Activity: coord,
	}

	tm := auth.NewTokenManager("test-secret-reconstruct-prod")
	svc := &handler.Services{
		DB:           sqldb,
		TokenManager: tm,
		Documents:    docSvc,
		Users:        auth.NewUserStore(sqldb),
	}
	router := handler.NewRouter(svc)

	// ── Step 1: Create a real user and get a JWT ───────────────────────────
	email := fmt.Sprintf("priya-%s@example.test", uuid.NewString()[:8])
	token, userID := registerUser(t, router, email, "test-password-123")

	// ── Step 2: Create a document version (captures CSS snapshot in DB) ───
	vOld, err := docSvc.CreateResumeFromProfile(ctx, userID, "Resume v1", documents.RenderContext{Language: "en"})
	require.NoError(t, err)
	require.Equal(t, int32(1), capturer.count.Load(), "initial render must call renderer once")

	// Confirm artifact blob was persisted.
	blobCountAfterCreate, err := countBlobFiles(blobRoot)
	require.NoError(t, err)
	require.Equal(t, 1, blobCountAfterCreate, "one artifact blob must exist after version creation")

	// Verify reuse counter is still zero (no PDFBytes call yet).
	assert.Zero(t, docMetrics.ReuseHits.Load())
	assert.Zero(t, docMetrics.Reconstructions.Load())

	// ── Step 3: Record the CSS snapshot stored in the DB ──────────────────
	var cssSnapshot string
	require.NoError(t, sqldb.QueryRowContext(ctx,
		`SELECT css_snapshot FROM document_content_versions WHERE id = ?`, vOld,
	).Scan(&cssSnapshot))
	require.NotEmpty(t, cssSnapshot, "css_snapshot must be persisted with the version")

	// ── Step 4: Insert a jobs_applied row and filler rows for eviction ────
	oldExportRel := filepath.ToSlash(filepath.Join("job_applications", userID, "resume-old-"+uuid.NewString()+".pdf"))
	oldExportAbs := filepath.Join(root, oldExportRel)
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

	for i := range domain.DocumentRetentionMin {
		ts := time.Date(2026, 1, 1, 10+i, 0, 0, 0, time.UTC).Format(time.RFC3339)
		_, err = sqldb.ExecContext(ctx, `
			INSERT INTO jobs_applied (id, user_id, platform, company, role, link, applied_at)
			VALUES (?, ?, 'seek', 'FillerCo', 'Role', 'https://example.com/f', ?)`,
			uuid.NewString(), userID, ts)
		require.NoError(t, err)
	}

	// ── Step 5: Run eviction ───────────────────────────────────────────────
	metrics, err := retSvc.RunEviction(ctx, userID, 10)
	require.NoError(t, err)
	require.GreaterOrEqual(t, metrics.PathsEvicted, 1)

	// Export file gone from disk, resume_path cleared in DB.
	_, statErr := os.Stat(oldExportAbs)
	require.True(t, os.IsNotExist(statErr), "evicted export file must be deleted")

	var resumePath string
	require.NoError(t, sqldb.QueryRowContext(ctx,
		`SELECT COALESCE(resume_path,'') FROM jobs_applied WHERE id = ?`, jobOldID,
	).Scan(&resumePath))
	require.Empty(t, resumePath)

	blobCountAfterEviction, err := countBlobFiles(blobRoot)
	require.NoError(t, err)
	// Artifact blob was also evicted in this run (one version, one blob).
	require.Less(t, blobCountAfterEviction, blobCountAfterCreate,
		"artifact blob must be evicted along with the export path")

	// ── Step 6: Mutate live profile and CSS inputs ─────────────────────────
	// If reconstruction used the live profile, the renderer would see this name.
	profile.PersonalInformation.Name = "Priya Live (updated)"
	rendersBeforeDownload := capturer.count.Load()

	// ── Step 7: Download via production route with ?token= ────────────────
	req := httptest.NewRequest(http.MethodGet,
		"/api/jobs/applied/"+jobOldID+"/pdf/resume?token="+token, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "download must succeed: %s", w.Body.String())
	require.Equal(t, "application/pdf", w.Header().Get("Content-Type"))
	require.Equal(t, "true", w.Header().Get("X-Jobifai-Reconstructed"),
		"evicted artifact must set X-Jobifai-Reconstructed: true")
	require.NotEmpty(t, w.Body.Bytes())

	// ── Step 8: Verify renderer got snapshot inputs, not live inputs ───────
	require.Equal(t, int32(1), capturer.count.Load()-rendersBeforeDownload,
		"download must trigger exactly one render (reconstruction)")

	call, ok := capturer.lastCall()
	require.True(t, ok)

	// The profile name in the renderer call must be the snapshot name, not the
	// updated live name.
	assert.Equal(t, "Priya Snapshot", call.profileName,
		"reconstruction must use stored snapshot profile, not live profile")
	assert.NotEqual(t, "Priya Live (updated)", call.profileName,
		"live profile update must not leak into reconstruction")

	// The CSS passed to the renderer must be the snapshot content.
	assert.Equal(t, cssSnapshot, call.cssContent,
		"reconstruction must pass the stored CSS snapshot to the renderer")

	// ── Step 9: Artifact blob must NOT be written back after reconstruction ─
	blobCountAfterDownload, err := countBlobFiles(blobRoot)
	require.NoError(t, err)
	assert.Equal(t, blobCountAfterEviction, blobCountAfterDownload,
		"reconstruction must not persist a new artifact blob")

	// ── Step 10: Metrics counters ──────────────────────────────────────────
	assert.Zero(t, docMetrics.ReuseHits.Load(), "no reuse hit expected (artifact was evicted)")
	assert.Equal(t, int64(1), docMetrics.Reconstructions.Load(), "one reconstruction expected")
	assert.Positive(t, docMetrics.ReconstructionBytesTotal.Load(), "reconstruction bytes must be recorded")

	// Serve again — this time the artifact is still absent (reconstruction does
	// not persist), so it is another reconstruction, not a reuse hit.
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, httptest.NewRequest(http.MethodGet,
		"/api/jobs/applied/"+jobOldID+"/pdf/resume?token="+token, nil))
	require.Equal(t, http.StatusOK, w2.Code)
	assert.Equal(t, int64(0), docMetrics.ReuseHits.Load(), "still no reuse: blob not persisted by reconstruction")
	assert.Equal(t, int64(2), docMetrics.Reconstructions.Load(), "second download is still a reconstruction")
}
