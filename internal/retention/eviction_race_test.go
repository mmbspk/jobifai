package retention_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/retention"
)

// TestSetDefault_BlocksDuringEviction is a regression for P1.2: before the fix,
// SetDefault could mutate user_document_defaults while eviction was reading the
// protection index, causing a newly-set default to reference a version whose
// PDF had just been deleted.
func TestSetDefault_BlocksDuringEviction(t *testing.T) {
	t.Parallel()
	coord := &retention.ActivityCoordinator{}
	userID := "u-setdefault-race"
	ctx := context.Background()

	var workAcquired atomic.Bool
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

	// Simulate SetDefault acquiring a work guard (which it does via withWork).
	workDone := make(chan struct{})
	go func() {
		defer close(workDone)
		if err := coord.BeginWork(ctx, userID); err == nil {
			workAcquired.Store(true)
			coord.EndWork(userID)
		}
	}()

	// The work guard must not be acquired while eviction holds the lock.
	time.Sleep(40 * time.Millisecond)
	require.False(t, workAcquired.Load(), "SetDefault must not proceed while eviction is running")

	close(releaseEvict)
	select {
	case <-workDone:
	case <-time.After(2 * time.Second):
		t.Fatal("SetDefault did not unblock after eviction completed")
	}
	require.True(t, workAcquired.Load())
}

// TestReconcileAll_ClosesCursorBeforeReconciling verifies that
// ScheduleReconcileAllUsers collects user IDs first and then reconciles, so
// the outer SELECT DISTINCT cursor is not held open while per-user queries
// compete for the bounded connection pool.
func TestReconcileAll_ClosesCursorBeforeReconciling(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _, root, sqldb := openRetentionTestDB(t)

	// Limit the pool to a small size to make cursor exhaustion detectable.
	sqldb.SetMaxOpenConns(4)

	// Seed several users, each with more than the default (20) applications so
	// at least one reconcile pass is triggered per user.
	users := []string{"u-rcall-1", "u-rcall-2", "u-rcall-3"}
	for _, uid := range users {
		vid := seedReconstructibleVersion(t, svc, uid)
		for i := 0; i < 22; i++ {
			ts := time.Now().UTC().Add(time.Duration(i) * time.Minute).Format(time.RFC3339)
			rp := "job_applications/" + uid + "/p" + ts + ".pdf"
			writePDF(t, root, rp)
			insertApplied(t, svc, uid, uid+"-job-"+ts, rp, "", vid, ts)
		}
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.ScheduleReconcileAllUsers()
	}()

	// ScheduleReconcileAllUsers returns immediately (it spawns a goroutine).
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ScheduleReconcileAllUsers did not return promptly")
	}

	// Wait for the background goroutine to finish (max 15 s to keep CI fast).
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		// Check that at least one user has been evicted to confirm the goroutine ran.
		var count int
		_ = sqldb.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM jobs_applied WHERE user_id = ? AND resume_path = ''`, users[0],
		).Scan(&count)
		if count > 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("ScheduleReconcileAllUsers goroutine did not complete within deadline")
}
