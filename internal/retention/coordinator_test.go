package retention_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/retention"
)

func TestActivityCoordinator_EvictionWaitsForActiveWork(t *testing.T) {
	t.Parallel()
	coord := &retention.ActivityCoordinator{}
	userID := "u-wait"
	ctx := context.Background()

	started := make(chan struct{})
	release := make(chan struct{})
	go func() {
		require.NoError(t, coord.BeginWork(ctx, userID))
		close(started)
		<-release
		coord.EndWork(userID)
	}()
	<-started

	done := make(chan struct{})
	var evictErr error
	go func() {
		evictErr = coord.WithEviction(ctx, userID, func() error {
			return nil
		})
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("eviction returned before work ended")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	<-done
	require.NoError(t, evictErr)
}

func TestActivityCoordinator_WorkBlocksDuringEviction(t *testing.T) {
	t.Parallel()
	coord := &retention.ActivityCoordinator{}
	userID := "u-block"
	ctx := context.Background()

	evicting := make(chan struct{})
	releaseEvict := make(chan struct{})
	go func() {
		_ = coord.WithEviction(ctx, userID, func() error {
			close(evicting)
			<-releaseEvict
			return nil
		})
	}()
	<-evicting

	var workStarted atomic.Bool
	workDone := make(chan struct{})
	go func() {
		_ = coord.BeginWork(ctx, userID)
		workStarted.Store(true)
		coord.EndWork(userID)
		close(workDone)
	}()

	time.Sleep(30 * time.Millisecond)
	require.False(t, workStarted.Load())
	close(releaseEvict)
	<-workDone
	require.True(t, workStarted.Load())
}

func TestActivityCoordinator_BeginWorkRespectsContextCancel(t *testing.T) {
	t.Parallel()
	coord := &retention.ActivityCoordinator{}
	userID := "u-cancel"
	ctx, cancel := context.WithCancel(context.Background())

	block := make(chan struct{})
	go func() {
		_ = coord.WithEviction(context.Background(), userID, func() error {
			<-block
			return nil
		})
	}()
	time.Sleep(20 * time.Millisecond)

	cancel()
	err := coord.BeginWork(ctx, userID)
	require.ErrorIs(t, err, context.Canceled)
	close(block)
}
