package bot

// White-box tests for liveBrowserMeter using a fake clock and a counting stub.
// These tests exercise: pre-paid bucket model, Pause/Resume, user_action_required,
// quota exhaustion, goroutine lifecycle, and idempotency key format.

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/quota"
)

// ─── stub MeterService ───────────────────────────────────────────────────────

type stubMeterSvc struct {
	mu      sync.Mutex
	before  []string // idempotency keys for BeforeBrowserIncrement calls (we pass "" sentinel)
	record  []string // idempotency keys passed to RecordBrowserIncrement
	inc     time.Duration
	// failImmediate: fail from the very first call (zero credits).
	// failAt > 0: fail when len(record) >= failAt (allow first N-1 successes).
	failImmediate bool
	failAt        int
	failErr       error // error to return; nil = quota.ErrExceeded
}

func (s *stubMeterSvc) checkFail() error {
	if s.failImmediate || (s.failAt > 0 && len(s.record) >= s.failAt) {
		if s.failErr != nil {
			return s.failErr
		}
		return quota.ErrExceeded
	}
	return nil
}

func (s *stubMeterSvc) BeforeBrowserIncrement(_ context.Context, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkFail(); err != nil {
		return err
	}
	s.before = append(s.before, "before")
	return nil
}

func (s *stubMeterSvc) RecordBrowserIncrement(_ context.Context, _, key, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkFail(); err != nil {
		return err
	}
	s.record = append(s.record, key)
	return nil
}

func (s *stubMeterSvc) BrowserIncrementDuration() (time.Duration, error) {
	if s.inc > 0 {
		return s.inc, nil
	}
	return 30 * time.Second, nil
}

func (s *stubMeterSvc) recordCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.record)
}

func (s *stubMeterSvc) keys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := make([]string, len(s.record))
	copy(r, s.record)
	return r
}

// ─── fake ticker ─────────────────────────────────────────────────────────────

// fakeTicker returns a manual-advance fake ticker. Call advance() to deliver one tick.
func fakeTicker() (ch chan time.Time, advance func(), stop func()) {
	ch = make(chan time.Time, 1)
	stopped := make(chan struct{})
	advance = func() { ch <- time.Now() }
	stop = func() {
		select {
		case <-stopped:
		default:
			close(stopped)
		}
	}
	return ch, advance, stop
}

func fakeTickerFactory(ch chan time.Time, stopFn func()) func(time.Duration) (<-chan time.Time, func()) {
	return func(time.Duration) (<-chan time.Time, func()) {
		return ch, stopFn
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func newMeter(svc *stubMeterSvc, newTicker func(time.Duration) (<-chan time.Time, func())) BrowserUsageMeter {
	return NewLiveBrowserMeter(svc, "u1", "run-abc", 30*time.Second, newTicker)
}

// ─── tests ────────────────────────────────────────────────────────────────────

// TestLiveBrowserMeter_FailedLaunchNoBilling verifies that if Activate is never called
// (e.g. launchBrowser fails), no billing events are emitted.
func TestLiveBrowserMeter_FailedLaunchNoBilling(t *testing.T) {
	t.Parallel()
	svc := &stubMeterSvc{}
	meter := newMeter(svc, nil)
	meter.Stop() // simulates cleanup path after failed launch
	assert.Nil(t, meter.Err())
	assert.Equal(t, 0, svc.recordCount())
}

// TestLiveBrowserMeter_ActivateChargesFirstBucket verifies pre-paid: one bucket is
// charged immediately on Activate, before any ticks fire.
func TestLiveBrowserMeter_ActivateChargesFirstBucket(t *testing.T) {
	t.Parallel()
	svc := &stubMeterSvc{}
	ch, _, stopFn := fakeTicker()
	meter := newMeter(svc, fakeTickerFactory(ch, stopFn))

	require.NoError(t, meter.Activate(context.Background()))
	assert.Equal(t, 1, svc.recordCount(), "exactly one bucket on Activate")
	assert.Equal(t, "browser-automation:u1:run-abc:0", svc.keys()[0])
	meter.Stop()
}

// TestLiveBrowserMeter_TickChargesNextBucket verifies a tick (billing increment elapsed)
// charges the next pre-paid bucket.
func TestLiveBrowserMeter_TickChargesNextBucket(t *testing.T) {
	t.Parallel()
	svc := &stubMeterSvc{}
	ch, advance, stopFn := fakeTicker()
	meter := newMeter(svc, fakeTickerFactory(ch, stopFn))

	require.NoError(t, meter.Activate(context.Background()))
	assert.Equal(t, 1, svc.recordCount())

	advance() // deliver one tick
	// Wait for the goroutine to process the tick.
	require.Eventually(t, func() bool { return svc.recordCount() == 2 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, "browser-automation:u1:run-abc:1", svc.keys()[1])
	meter.Stop()
}

// TestLiveBrowserMeter_MultipleTicksBucketIdx verifies the bucket index increments
// monotonically across multiple ticks.
func TestLiveBrowserMeter_MultipleTicksBucketIdx(t *testing.T) {
	t.Parallel()
	svc := &stubMeterSvc{}
	ch, advance, stopFn := fakeTicker()
	meter := newMeter(svc, fakeTickerFactory(ch, stopFn))

	require.NoError(t, meter.Activate(context.Background()))
	for i := 0; i < 3; i++ {
		advance()
	}
	require.Eventually(t, func() bool { return svc.recordCount() == 4 }, 2*time.Second, 10*time.Millisecond)

	keys := svc.keys()
	for i, k := range keys {
		assert.Equal(t, fmt.Sprintf("browser-automation:u1:run-abc:%d", i), k)
	}
	meter.Stop()
}

// TestLiveBrowserMeter_DeactivateStopsTicks verifies Deactivate stops the ticker goroutine
// so subsequent ticks are not billed.
func TestLiveBrowserMeter_DeactivateStopsTicks(t *testing.T) {
	t.Parallel()
	svc := &stubMeterSvc{}
	ch, advance, stopFn := fakeTicker()
	meter := newMeter(svc, fakeTickerFactory(ch, stopFn))

	require.NoError(t, meter.Activate(context.Background()))
	assert.Equal(t, 1, svc.recordCount())

	meter.Deactivate()
	advance() // tick after deactivate — should not bill
	// Give the goroutine time to wake up (if it's still running).
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 1, svc.recordCount(), "no billing after Deactivate")
	meter.Stop()
}

// TestLiveBrowserMeter_PauseResume verifies that:
// - Deactivate on Pause stops billing
// - Activate on Resume charges a new (non-duplicate) bucket
func TestLiveBrowserMeter_PauseResume(t *testing.T) {
	t.Parallel()
	svc := &stubMeterSvc{}
	ch1, _, stopFn1 := fakeTicker()
	ch2, _, stopFn2 := fakeTicker()
	call := atomic.Int32{}
	newTicker := func(d time.Duration) (<-chan time.Time, func()) {
		if call.Add(1) == 1 {
			return ch1, stopFn1
		}
		return ch2, stopFn2
	}
	meter := newMeter(svc, newTicker)

	// First activation (pre-run or auth succeeds).
	require.NoError(t, meter.Activate(context.Background()))
	assert.Equal(t, 1, svc.recordCount())
	assert.Equal(t, "browser-automation:u1:run-abc:0", svc.keys()[0])

	// Pause — Deactivate stops billing.
	meter.Deactivate()

	// Resume — Activate charges a new bucket (index 1, not 0 again).
	require.NoError(t, meter.Activate(context.Background()))
	assert.Equal(t, 2, svc.recordCount())
	assert.Equal(t, "browser-automation:u1:run-abc:1", svc.keys()[1])

	meter.Stop()
}

// TestLiveBrowserMeter_QuotaExhaustion verifies that ErrExceeded from the billing service
// propagates through Err() and further Activate calls return the error.
func TestLiveBrowserMeter_QuotaExhaustion(t *testing.T) {
	t.Parallel()
	svc := &stubMeterSvc{failImmediate: true} // fail from the first call (zero balance)
	meter := newMeter(svc, nil)

	err := meter.Activate(context.Background())
	require.Error(t, err)
	assert.True(t, errors.Is(err, quota.ErrExceeded))
	assert.True(t, errors.Is(meter.Err(), quota.ErrExceeded))

	// Subsequent Activate returns stored error without billing.
	err2 := meter.Activate(context.Background())
	assert.True(t, errors.Is(err2, quota.ErrExceeded))
	assert.Equal(t, 0, svc.recordCount(), "no billing after ErrExceeded")
}

// TestLiveBrowserMeter_QuotaExhaustionOnTick verifies that ErrExceeded during a tick
// (second bucket charge) sets Err() and the goroutine stops.
func TestLiveBrowserMeter_QuotaExhaustionOnTick(t *testing.T) {
	t.Parallel()
	svc := &stubMeterSvc{failAt: 1} // allow first bucket, fail on second
	ch, advance, stopFn := fakeTicker()
	meter := newMeter(svc, fakeTickerFactory(ch, stopFn))

	require.NoError(t, meter.Activate(context.Background()))
	assert.Nil(t, meter.Err())

	advance() // second bucket → quota exhausted
	require.Eventually(t, func() bool { return meter.Err() != nil }, 2*time.Second, 10*time.Millisecond)
	assert.True(t, errors.Is(meter.Err(), quota.ErrExceeded))
	assert.Equal(t, 1, svc.recordCount(), "only first bucket charged")

	meter.Stop()
}

// TestLiveBrowserMeter_StopPreventsActivate verifies that after Stop(), Activate is a
// no-op and returns nil.
func TestLiveBrowserMeter_StopPreventsActivate(t *testing.T) {
	t.Parallel()
	svc := &stubMeterSvc{}
	meter := newMeter(svc, nil)

	meter.Stop()
	require.NoError(t, meter.Activate(context.Background()))
	assert.Equal(t, 0, svc.recordCount(), "no billing after Stop")
}

// TestLiveBrowserMeter_IdempotencyKeyUnique verifies that every bucket across Pause/Resume
// cycles receives a unique, monotonically increasing idempotency key.
func TestLiveBrowserMeter_IdempotencyKeyUnique(t *testing.T) {
	t.Parallel()
	svc := &stubMeterSvc{}
	var tickers []chan time.Time
	newTicker := func(d time.Duration) (<-chan time.Time, func()) {
		ch := make(chan time.Time, 1)
		tickers = append(tickers, ch)
		return ch, func() {}
	}
	meter := newMeter(svc, newTicker)

	for cycle := 0; cycle < 3; cycle++ {
		require.NoError(t, meter.Activate(context.Background()))
		meter.Deactivate()
	}

	keys := svc.keys()
	require.Len(t, keys, 3)
	seen := map[string]bool{}
	for _, k := range keys {
		assert.False(t, seen[k], "duplicate key: %s", k)
		seen[k] = true
	}
	// Keys must be ordered 0, 1, 2.
	assert.Equal(t, "browser-automation:u1:run-abc:0", keys[0])
	assert.Equal(t, "browser-automation:u1:run-abc:1", keys[1])
	assert.Equal(t, "browser-automation:u1:run-abc:2", keys[2])
	meter.Stop()
}

// TestLiveBrowserMeter_NoGoroutineLeak verifies that after Stop(), no goroutine is left
// running by checking the ticker stop function is called.
func TestLiveBrowserMeter_NoGoroutineLeak(t *testing.T) {
	t.Parallel()
	svc := &stubMeterSvc{}
	stopCalled := atomic.Int32{}
	ch := make(chan time.Time)
	newTicker := func(d time.Duration) (<-chan time.Time, func()) {
		return ch, func() { stopCalled.Add(1) }
	}
	meter := newMeter(svc, newTicker)

	require.NoError(t, meter.Activate(context.Background()))
	meter.Stop()
	// Give the goroutine a moment to exit.
	require.Eventually(t, func() bool { return stopCalled.Load() > 0 }, 2*time.Second, 10*time.Millisecond)
}

// TestLiveBrowserMeter_ConcurrentActivateStop verifies concurrent Activate/Stop are safe.
func TestLiveBrowserMeter_ConcurrentActivateStop(t *testing.T) {
	t.Parallel()
	svc := &stubMeterSvc{}
	meter := newMeter(svc, nil)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			if i%2 == 0 {
				_ = meter.Activate(context.Background())
			} else {
				meter.Stop()
			}
		}()
	}
	wg.Wait()
	// Should not panic or deadlock; billing count may be any value.
}

// TestLiveBrowserMeter_UserActionRequired simulates the user_action_required pattern:
// Deactivate during the wait, Activate after the user re-authenticates.
func TestLiveBrowserMeter_UserActionRequired(t *testing.T) {
	t.Parallel()
	svc := &stubMeterSvc{}
	var call atomic.Int32
	newTicker := func(d time.Duration) (<-chan time.Time, func()) {
		call.Add(1)
		return make(chan time.Time), func() {}
	}
	meter := newMeter(svc, newTicker)

	// Bot starts: auth succeeds, billing activated.
	require.NoError(t, meter.Activate(context.Background()))
	assert.Equal(t, 1, svc.recordCount())

	// Mid-run auth challenge: Deactivate (stops billing).
	meter.Deactivate()
	assert.Equal(t, int32(1), call.Load(), "one goroutine started")

	// User re-authenticates: Activate again (new bucket).
	require.NoError(t, meter.Activate(context.Background()))
	assert.Equal(t, 2, svc.recordCount(), "second bucket after re-auth")
	assert.Equal(t, "browser-automation:u1:run-abc:1", svc.keys()[1])

	meter.Stop()
}

// TestLiveBrowserMeter_ReviewWait simulates the Review approval wait:
// billing is deactivated during the wait and reactivated when approved.
func TestLiveBrowserMeter_ReviewWait(t *testing.T) {
	t.Parallel()
	// Same pattern as user_action_required: Deactivate → wait → Activate.
	svc := &stubMeterSvc{}
	meter := newMeter(svc, nil)

	require.NoError(t, meter.Activate(context.Background()))
	meter.Deactivate() // Review wait begins
	assert.Equal(t, 1, svc.recordCount())

	require.NoError(t, meter.Activate(context.Background())) // Review approved
	assert.Equal(t, 2, svc.recordCount())

	meter.Stop()
}

// TestLiveBrowserMeter_PersistentBrowserIdleNoBilling verifies that constructing a meter
// but never calling Activate (e.g. persistent browser idle) results in 0 billing events.
func TestLiveBrowserMeter_PersistentBrowserIdleNoBilling(t *testing.T) {
	t.Parallel()
	svc := &stubMeterSvc{}
	meter := newMeter(svc, nil)
	// Meter created but never activated.
	meter.Stop()
	assert.Equal(t, 0, svc.recordCount())
	assert.Nil(t, meter.Err())
}

// TestLiveBrowserMeter_ContextCancelledStopsGoroutine verifies that when the run context
// is cancelled, the ticker goroutine exits cleanly.
func TestLiveBrowserMeter_ContextCancelledStopsGoroutine(t *testing.T) {
	t.Parallel()
	svc := &stubMeterSvc{}
	stopCalled := atomic.Int32{}
	ch := make(chan time.Time)
	newTicker := func(d time.Duration) (<-chan time.Time, func()) {
		return ch, func() { stopCalled.Add(1) }
	}
	ctx, cancel := context.WithCancel(context.Background())
	meter := NewLiveBrowserMeter(svc, "u1", "run-ctx", 30*time.Second, newTicker)

	require.NoError(t, meter.Activate(ctx))
	cancel() // cancel the run context
	require.Eventually(t, func() bool { return stopCalled.Load() > 0 }, 2*time.Second, 10*time.Millisecond)
}
