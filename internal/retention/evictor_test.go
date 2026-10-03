package retention_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/config"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/retention"
)

func openRetentionTestDB(t *testing.T) (*retention.Service, *config.Store, string) {
	t.Helper()
	root := t.TempDir()
	dbPath := filepath.Join(root, "test.db")
	sqldb, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })
	cfg := config.NewStore(sqldb)
	svc := &retention.Service{DB: sqldb, Config: cfg, Root: root}
	return svc, cfg, root
}

func insertApplied(t *testing.T, svc *retention.Service, userID, jobID, resumePath, coverPath, appliedAt string) {
	t.Helper()
	_, err := svc.DB.ExecContext(context.Background(), `
		INSERT INTO jobs_applied (id, user_id, platform, company, role, link, resume_path, cover_letter_path, applied_at)
		VALUES (?, ?, 'seek', 'Co', 'Role', 'https://example.com/j', ?, ?, ?)`,
		jobID, userID, resumePath, coverPath, appliedAt)
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
	svc, _, root := openRetentionTestDB(t)
	userID := "user-retain-20"
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 21; i++ {
		ts := base.Add(time.Duration(i) * time.Hour).Format(time.RFC3339)
		rp := filepath.ToSlash(filepath.Join("job_applications", userID, "job-"+uuid.NewString()+".pdf"))
		writePDF(t, root, rp)
		insertApplied(t, svc, userID, uuid.NewString(), rp, "", ts)
	}
	metrics, err := svc.RunEviction(ctx, userID, 50)
	require.NoError(t, err)
	require.Equal(t, 1, metrics.ApplicationsScanned)
	require.Equal(t, 1, metrics.PathsEvicted)

	var remaining int
	require.NoError(t, svc.DB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM jobs_applied WHERE user_id = ? AND COALESCE(resume_path,'') != ''`, userID).Scan(&remaining))
	require.Equal(t, 20, remaining)
}

func TestService_RunEviction_AdminLimit5To30(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, cfg, root := openRetentionTestDB(t)
	userID := "user-limit-change"
	require.NoError(t, retention.SaveDefaults(cfg, domain.DocumentRetentionDefaults{LatestSubmittedApplications: 5}))
	base := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 10; i++ {
		ts := base.Add(time.Duration(i) * time.Hour).Format(time.RFC3339)
		rp := filepath.ToSlash(filepath.Join("job_applications", userID, "r"+uuid.NewString()+".pdf"))
		writePDF(t, root, rp)
		insertApplied(t, svc, userID, uuid.NewString(), rp, "", ts)
	}
	m1, err := svc.RunEviction(ctx, userID, 50)
	require.NoError(t, err)
	require.Equal(t, 5, m1.ApplicationsScanned)

	_, err = retention.SaveDefaultsAudited(ctx, cfg, svc.DB, "admin-1", domain.DocumentRetentionDefaults{LatestSubmittedApplications: 30})
	require.NoError(t, err)
	require.Equal(t, 30, svc.Limit())

	m2, err := svc.RunEviction(ctx, userID, 50)
	require.NoError(t, err)
	require.Equal(t, 0, m2.ApplicationsScanned)
}

func TestService_RunEviction_ProtectsSharedPathWithRetainedJob(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _, root := openRetentionTestDB(t)
	userID := "user-shared"
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
		insertApplied(t, svc, userID, uuid.NewString(), rp, "", ts)
	}
	metrics, err := svc.RunEviction(ctx, userID, 50)
	require.NoError(t, err)
	require.GreaterOrEqual(t, metrics.PathsProtected, 1)
	_, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(shared)))
	require.NoError(t, statErr)
}

func TestService_RunEviction_DeleteFailureIncrementsMetric(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _, root := openRetentionTestDB(t)
	userID := "user-del-fail"
	base := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 21; i++ {
		ts := base.Add(time.Duration(i) * time.Hour).Format(time.RFC3339)
		rp := filepath.ToSlash(filepath.Join("job_applications", userID, "x"+uuid.NewString()+".pdf"))
		writePDF(t, root, rp)
		insertApplied(t, svc, userID, uuid.NewString(), rp, "", ts)
	}
	// Remove write permission on directory so deletes fail for oldest evictable files.
	dir := filepath.Join(root, "job_applications", userID)
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	metrics, err := svc.RunEviction(ctx, userID, 50)
	require.NoError(t, err)
	require.Equal(t, 1, metrics.DeleteFailures)
}

func TestService_PreviewEviction_UserIsolation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _, root := openRetentionTestDB(t)
	u1, u2 := "user-a", "user-b"
	for _, u := range []string{u1, u2} {
		for i := 0; i < 21; i++ {
			ts := time.Now().UTC().Add(time.Duration(i) * time.Minute).Format(time.RFC3339)
			rp := filepath.ToSlash(filepath.Join("job_applications", u, "p"+uuid.NewString()+".pdf"))
			writePDF(t, root, rp)
			insertApplied(t, svc, u, uuid.NewString(), rp, "", ts)
		}
	}
	cands, _, err := svc.PreviewEviction(ctx, u1)
	require.NoError(t, err)
	require.Len(t, cands, 1)
}
