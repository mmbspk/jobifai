package retention_test

import (
	"context"
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

// countingRendererRace is a minimal PDFRenderer used by the race tests.
type countingRendererRace struct{ calls atomic.Int32 }

func (r *countingRendererRace) RenderResume(_ context.Context, _ *domain.ResumeProfile, _, _ string, _ *resume.RenderOptions) ([]byte, error) {
	r.calls.Add(1)
	return []byte("%PDF-race"), nil
}
func (r *countingRendererRace) RenderCoverLetter(_ context.Context, _ string, _, _ string) ([]byte, error) {
	r.calls.Add(1)
	return []byte("%PDF-race-cover"), nil
}

// openDocService builds a documents.Service backed by its own fresh SQLite DB,
// sharing the ActivityCoordinator with svc so the work/eviction guards interlock.
func openDocService(t *testing.T, coord *retention.ActivityCoordinator) (*documents.Service, string) {
	t.Helper()
	root := t.TempDir()
	dbPath := filepath.Join(root, "doctest.db")
	sqldb, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })
	blobs, err := documents.NewLocalBlobStore(filepath.Join(root, "blobs"))
	require.NoError(t, err)
	profile := &domain.ResumeProfile{
		PersonalInformation: domain.PersonalInformation{Name: "Race Tester", Email: "race@test.example"},
		Summary:             "Regression profile for guard tests.",
	}
	svc := &documents.Service{
		Store:     documents.NewStore(sqldb),
		Blobs:     blobs,
		Renderer:  &countingRendererRace{},
		MarketDir: filepath.Join("..", "..", "resume_markets"),
		StylesDir: filepath.Join("..", "..", documents.StylesDirRelative),
		LoadProfile: func(_ string) (*domain.ResumeProfile, error) {
			return profile, nil
		},
		WorkGuard: coord,
	}
	return svc, root
}

// TestSetDefault_BlocksDuringEviction is a regression for the P1.2 write-guard
// finding: SetDefault (via withWork) must not proceed while eviction holds the
// ActivityCoordinator lock for the same user.
func TestSetDefault_BlocksDuringEviction(t *testing.T) {
	t.Parallel()
	coord := &retention.ActivityCoordinator{}
	docSvc, _ := openDocService(t, coord)
	ctx := context.Background()
	userID := "u-setdefault-race"

	// Create a version that SetDefault can target.
	vID, err := docSvc.CreateResumeFromProfile(ctx, userID, "Race resume", documents.RenderContext{Language: "en"})
	require.NoError(t, err)

	evicting := make(chan struct{})
	releaseEvict := make(chan struct{})

	go func() {
		_ = coord.WithEviction(context.Background(), userID, func() error {
			close(evicting)
			<-releaseEvict
			return nil
		})
	}()
	<-evicting

	var setDefaultDone atomic.Bool
	workDone := make(chan struct{})
	go func() {
		defer close(workDone)
		// SetDefault internally calls withWork → BeginWork, which must block
		// while eviction holds the lock.
		_ = docSvc.SetDefault(ctx, userID, documents.KindResume, vID)
		setDefaultDone.Store(true)
	}()

	// The write must not complete while eviction holds the lock.
	time.Sleep(40 * time.Millisecond)
	require.False(t, setDefaultDone.Load(), "SetDefault must not proceed while eviction is running")

	close(releaseEvict)
	select {
	case <-workDone:
	case <-time.After(2 * time.Second):
		t.Fatal("SetDefault did not unblock after eviction completed")
	}
	require.True(t, setDefaultDone.Load())
}

// TestReconcileAll_ClosesCursorBeforeReconciling verifies that
// ScheduleReconcileAllUsers collects user IDs first and then reconciles, so
// the outer SELECT DISTINCT cursor is not held open while per-user queries
// compete for the bounded connection pool.  With a pool of 4 connections and
// nested queries per user, holding the outer cursor would deadlock.
func TestReconcileAll_ClosesCursorBeforeReconciling(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _, root, sqldb := openRetentionTestDB(t)

	// Limit the pool to a small size to make cursor-hold deadlock detectable.
	sqldb.SetMaxOpenConns(4)

	// Seed several users, each with more than the default (20) applications so
	// at least one reconcile pass is triggered per user.
	users := []string{"u-rcall-1", "u-rcall-2", "u-rcall-3"}
	for _, uid := range users {
		vid := seedReconstructibleVersion(t, svc, uid)
		for i := 0; i < 22; i++ {
			ts := time.Now().UTC().Add(time.Duration(i) * time.Minute).Format(time.RFC3339)
			rp := "job_applications/" + uid + "/p" + uuid.NewString() + ".pdf"
			writePDF(t, root, rp)
			insertApplied(t, svc, uid, uid+"-job-"+ts, rp, "", vid, ts)
		}
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.ScheduleReconcileAllUsers()
	}()

	// ScheduleReconcileAllUsers must return promptly (it spawns a goroutine).
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ScheduleReconcileAllUsers did not return promptly")
	}

	// Wait for the background goroutine to finish (max 20 s).
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		// Verify all three users were processed: each must have evicted paths.
		allDone := true
		for _, uid := range users {
			var count int
			_ = sqldb.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM jobs_applied WHERE user_id = ? AND resume_path = ''`, uid,
			).Scan(&count)
			if count == 0 {
				allDone = false
				break
			}
		}
		if allDone {
			return
		}
	}
	t.Fatal("ScheduleReconcileAllUsers goroutine did not complete all users within deadline")
}
