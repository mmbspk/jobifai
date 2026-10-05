package retention_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/config"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/documents"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/retention"
)

func openRetentionTestDB(t *testing.T) (*retention.Service, *config.Store, string, *sql.DB) {
	t.Helper()
	root := t.TempDir()
	dbPath := filepath.Join(root, "test.db")
	sqldb, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })
	cfg := config.NewStore(sqldb)
	blobs, err := documents.NewLocalBlobStore(filepath.Join(root, "blobs"))
	require.NoError(t, err)
	coord := &retention.ActivityCoordinator{}
	svc := &retention.Service{DB: sqldb, Config: cfg, Root: root, Blobs: blobs, Activity: coord}
	return svc, cfg, root, sqldb
}

func seedReconstructibleVersion(t *testing.T, svc *retention.Service, userID string) string {
	t.Helper()
	docID := uuid.NewString()
	vid := uuid.NewString()
	_, err := svc.DB.ExecContext(context.Background(), `
		INSERT INTO user_documents (id, user_id, kind, title) VALUES (?, ?, 'resume', 't')`,
		docID, userID)
	require.NoError(t, err)
	_, err = svc.DB.ExecContext(context.Background(), `
		INSERT INTO document_content_versions (
			id, document_id, user_id, version_number, source, content_kind, content_json,
			css_snapshot, renderer_version, render_snapshot_json, reconstructible
		) VALUES (?, ?, ?, 1, 'test', 'resume_json', '{}', 'css', 'v1', '{}', 1)`,
		vid, docID, userID)
	require.NoError(t, err)
	return vid
}

func insertApplied(t *testing.T, svc *retention.Service, userID, jobID, resumePath, coverPath, resumeVID, appliedAt string) {
	t.Helper()
	_, err := svc.DB.ExecContext(context.Background(), `
		INSERT INTO jobs_applied (id, user_id, platform, company, role, link, resume_path, cover_letter_path,
		 resume_content_version_id, applied_at)
		VALUES (?, ?, 'seek', 'Co', 'Role', 'https://example.com/j', ?, ?, ?, ?)`,
		jobID, userID, resumePath, coverPath, resumeVID, appliedAt)
	require.NoError(t, err)
}

func writePDF(t *testing.T, root, relPath string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(relPath))
	require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
	require.NoError(t, os.WriteFile(abs, []byte("%PDF-test"), 0o644))
}

func TestService_RunEviction_21Applications_KeepsLatest20(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _, root, _ := openRetentionTestDB(t)
	userID := "user-retain-20"
	vid := seedReconstructibleVersion(t, svc, userID)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 21; i++ {
		ts := base.Add(time.Duration(i) * time.Hour).Format(time.RFC3339)
		rp := filepath.ToSlash(filepath.Join("job_applications", userID, "job-"+uuid.NewString()+".pdf"))
		writePDF(t, root, rp)
		insertApplied(t, svc, userID, uuid.NewString(), rp, "", vid, ts)
	}
	metrics, err := svc.ReconcileUser(ctx, userID)
	require.NoError(t, err)
	require.GreaterOrEqual(t, metrics.PathsEvicted, 1)

	var remaining int
	require.NoError(t, svc.DB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM jobs_applied WHERE user_id = ? AND COALESCE(resume_path,'') != ''`, userID).Scan(&remaining))
	require.Equal(t, 20, remaining)
}

func TestService_RunEviction_SkipsWithoutReconstructibleVersion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _, root, _ := openRetentionTestDB(t)
	userID := "user-no-recon"
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 21; i++ {
		ts := base.Add(time.Duration(i) * time.Hour).Format(time.RFC3339)
		rp := filepath.ToSlash(filepath.Join("job_applications", userID, "job-"+uuid.NewString()+".pdf"))
		writePDF(t, root, rp)
		insertApplied(t, svc, userID, uuid.NewString(), rp, "", "", ts)
	}
	metrics, err := svc.RunEviction(ctx, userID, 50)
	require.NoError(t, err)
	require.Equal(t, 0, metrics.PathsEvicted)
	require.Greater(t, metrics.PathsProtected, 0)
}

func TestService_RunEviction_AdminLimit5To30(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, cfg, root, sqldb := openRetentionTestDB(t)
	userID := "user-limit-change"
	vid := seedReconstructibleVersion(t, svc, userID)
	require.NoError(t, retention.SaveDefaults(cfg, domain.DocumentRetentionDefaults{LatestSubmittedApplications: 5}))
	base := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 10; i++ {
		ts := base.Add(time.Duration(i) * time.Hour).Format(time.RFC3339)
		rp := filepath.ToSlash(filepath.Join("job_applications", userID, "r"+uuid.NewString()+".pdf"))
		writePDF(t, root, rp)
		insertApplied(t, svc, userID, uuid.NewString(), rp, "", vid, ts)
	}
	m1, err := svc.RunEviction(ctx, userID, 50)
	require.NoError(t, err)
	require.Equal(t, 5, m1.ApplicationsScanned)

	_, err = retention.SaveDefaultsAudited(ctx, cfg, sqldb, "admin-1", domain.DocumentRetentionDefaults{LatestSubmittedApplications: 30})
	require.NoError(t, err)
	require.Equal(t, 30, svc.Limit())

	m2, err := svc.RunEviction(ctx, userID, 50)
	require.NoError(t, err)
	require.Equal(t, 0, m2.ApplicationsScanned)
}

func TestService_RunEviction_ProtectsSharedPathWithRetainedJob(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _, root, _ := openRetentionTestDB(t)
	userID := "user-shared"
	vid := seedReconstructibleVersion(t, svc, userID)
	shared := filepath.ToSlash(filepath.Join("job_applications", userID, "shared.pdf"))
	writePDF(t, root, shared)
	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 21; i++ {
		ts := base.Add(time.Duration(i) * time.Hour).Format(time.RFC3339)
		rp := filepath.ToSlash(filepath.Join("job_applications", userID, "solo-"+uuid.NewString()+".pdf"))
		switch i {
		case 0, 15:
			rp = shared
		default:
			writePDF(t, root, rp)
		}
		insertApplied(t, svc, userID, uuid.NewString(), rp, "", vid, ts)
	}
	metrics, err := svc.RunEviction(ctx, userID, 50)
	require.NoError(t, err)
	require.GreaterOrEqual(t, metrics.PathsProtected, 1)
	_, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(shared)))
	require.NoError(t, statErr)
}

type faultQueryDB struct {
	retention.DB
	failOnSubstring string
}

func (f *faultQueryDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if f.failOnSubstring != "" && strings.Contains(query, f.failOnSubstring) {
		return nil, errors.New("simulated defaults query failure")
	}
	return f.DB.QueryContext(ctx, query, args...)
}

func seedArtifactVersion(t *testing.T, svc *retention.Service, userID, artifactKey string) string {
	t.Helper()
	docID := uuid.NewString()
	vid := uuid.NewString()
	artID := uuid.NewString()
	_, err := svc.DB.ExecContext(context.Background(), `
		INSERT INTO user_documents (id, user_id, kind, title) VALUES (?, ?, 'resume', 't')`,
		docID, userID)
	require.NoError(t, err)
	_, err = svc.DB.ExecContext(context.Background(), `
		INSERT INTO document_content_versions (
			id, document_id, user_id, version_number, source, content_kind, content_json,
			css_snapshot, renderer_version, reconstructible
		) VALUES (?, ?, ?, 1, 'test', 'resume_json', '{}', 'css', 'v1', 1)`,
		vid, docID, userID)
	require.NoError(t, err)
	_, err = svc.DB.ExecContext(context.Background(), `
		INSERT INTO document_render_artifacts (
			id, user_id, content_version_id, storage_key, sha256, byte_size, renderer_version, template_identity, state
		) VALUES (?, ?, ?, ?, 'sha-test', 9, 'v1', 'tpl', 'ready')`,
		artID, userID, vid, artifactKey)
	require.NoError(t, err)
	_, err = svc.DB.ExecContext(context.Background(), `
		INSERT INTO document_version_artifact_refs (content_version_id, artifact_id) VALUES (?, ?)`,
		vid, artID)
	require.NoError(t, err)
	return vid
}

func TestService_ArtifactEviction_AbortsWhenDefaultsQueryFails(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _, root, _ := openRetentionTestDB(t)
	userID := "user-art-db-fail"
	artifactKey := filepath.ToSlash(filepath.Join("artifacts", userID, "orphan.pdf"))
	absBlob := filepath.Join(root, "blobs", filepath.FromSlash(artifactKey))
	require.NoError(t, os.MkdirAll(filepath.Dir(absBlob), 0o755))
	require.NoError(t, os.WriteFile(absBlob, []byte("blob"), 0o644))
	seedArtifactVersion(t, svc, userID, artifactKey)

	inner := svc.DB
	svc.DB = &faultQueryDB{DB: inner, failOnSubstring: "user_document_defaults"}
	_, err := svc.RunEviction(ctx, userID, 50)
	require.Error(t, err)
	_, statErr := os.Stat(absBlob)
	require.NoError(t, statErr)
}

func TestService_ArtifactEviction_ProtectsUserDefaultVersionBlob(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _, root, _ := openRetentionTestDB(t)
	userID := "user-default-art"
	artifactKey := filepath.ToSlash(filepath.Join("artifacts", userID, "default.pdf"))
	absBlob := filepath.Join(root, "blobs", filepath.FromSlash(artifactKey))
	require.NoError(t, os.MkdirAll(filepath.Dir(absBlob), 0o755))
	require.NoError(t, os.WriteFile(absBlob, []byte("default-blob"), 0o644))
	vid := seedArtifactVersion(t, svc, userID, artifactKey)
	_, err := svc.DB.ExecContext(ctx, `
		INSERT INTO user_document_defaults (user_id, kind, content_version_id)
		VALUES (?, 'resume', ?)`, userID, vid)
	require.NoError(t, err)

	metrics, err := svc.RunEviction(ctx, userID, 50)
	require.NoError(t, err)
	require.Equal(t, 0, metrics.ArtifactBlobsEvicted)
	_, statErr := os.Stat(absBlob)
	require.NoError(t, statErr)
}

func TestService_PreviewEviction_UserIsolation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _, root, _ := openRetentionTestDB(t)
	u1, u2 := "user-a", "user-b"
	for _, u := range []string{u1, u2} {
		vid := seedReconstructibleVersion(t, svc, u)
		for i := 0; i < 21; i++ {
			ts := time.Now().UTC().Add(time.Duration(i) * time.Minute).Format(time.RFC3339)
			rp := filepath.ToSlash(filepath.Join("job_applications", u, "p"+uuid.NewString()+".pdf"))
			writePDF(t, root, rp)
			insertApplied(t, svc, u, uuid.NewString(), rp, "", vid, ts)
		}
	}
	cands, _, err := svc.PreviewEviction(ctx, u1)
	require.NoError(t, err)
	require.Len(t, cands, 1)
}
