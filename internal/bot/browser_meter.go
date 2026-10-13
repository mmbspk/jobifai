package bot

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// MeterService is the subset of *resource.Service used for browser automation billing.
// resource.Service satisfies this interface without importing this package.
type MeterService interface {
	BeforeBrowserIncrement(ctx context.Context, userID string) error
	// RecordBrowserIncrement atomically commits one billing increment.
	// durationMS is the length of the billed interval; jobID may be empty for
	// multi-application runs where the job is not known at charge time.
	RecordBrowserIncrement(ctx context.Context, userID, idempotencyKey, automationRunID, jobID string, durationMS int64) error
	BrowserIncrementDuration() (time.Duration, error)
}

// BrowserUsageMeter meters active browser automation runtime against the credit balance.
//
// Lifecycle:
//   - Activate: charge one bucket immediately (pre-paid) and start the tick goroutine
//     for subsequent buckets. May be called again after Deactivate (Pause/Resume cycle).
//   - Deactivate: stop the tick goroutine; the already-charged bucket is non-refundable.
//   - Err: returns quota.ErrExceeded when the balance is insufficient; checked by stopReason.
//   - Stop: permanent termination; Activate becomes a no-op.
//   - BillingCtx: returns a context that is cancelled when quota is exhausted. Active
//     automation may select on this context to stop mid-flow without reaching a submit.
//   - SetJobID: records the current job ID for correlation in billing events (single-
//     application paths only; ignored on multi-application runs).
//   - WasActivated: reports whether Activate was ever called successfully (used to decide
//     whether to re-activate after a mid-run session refresh).
type BrowserUsageMeter interface {
	Activate(ctx context.Context) error
	Deactivate()
	Err() error
	Stop()
	BillingCtx() context.Context
	SetJobID(jobID string)
	WasActivated() bool
}

// NewLiveBrowserMeter creates a pre-paid bucket meter backed by svc.
// increment is the billing period (0 → 30 s default).
// newTicker is injectable for tests; pass nil for the real time.NewTicker.
func NewLiveBrowserMeter(
	svc MeterService,
	userID, runID string,
	increment time.Duration,
	newTicker func(time.Duration) (<-chan time.Time, func()),
) BrowserUsageMeter {
	if increment <= 0 {
		increment = 30 * time.Second
	}
	if newTicker == nil {
		newTicker = func(d time.Duration) (<-chan time.Time, func()) {
			t := time.NewTicker(d)
			return t.C, t.Stop
		}
	}
	return &liveBrowserMeter{
		svc:       svc,
		userID:    userID,
		runID:     runID,
		increment: increment,
		newTicker: newTicker,
	}
}

type liveBrowserMeter struct {
	mu            sync.Mutex
	stopped       bool
	everActivated bool
	err           error
	bucketIdx     int64
	userID        string
	runID         string
	jobID         string
	increment     time.Duration
	svc           MeterService
	newTicker     func(time.Duration) (<-chan time.Time, func())
	cancelTick    context.CancelFunc
	billingCtx    context.Context
	billingCancel context.CancelFunc
}

// Activate charges the current bucket (pre-paid) and starts the tick goroutine.
// Safe to call multiple times on Pause/Resume cycles.
func (m *liveBrowserMeter) Activate(ctx context.Context) error {
	// Phase 1: snapshot and validate state under lock.
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return nil
	}
	if m.err != nil {
		err := m.err
		m.mu.Unlock()
		return err
	}
	// Stop any previous tick goroutine.
	if m.cancelTick != nil {
		m.cancelTick()
		m.cancelTick = nil
	}
	// Create a fresh billing context for this activation cycle.
	// Cancelling billingCtx signals mid-flow automation to stop.
	if m.billingCancel != nil {
		m.billingCancel()
	}
	billingCtx, billingCancel := context.WithCancel(ctx)
	m.billingCtx = billingCtx
	m.billingCancel = billingCancel
	idx := m.bucketIdx
	m.bucketIdx++
	jobID := m.jobID
	durationMS := m.increment.Milliseconds()
	m.mu.Unlock()

	// Phase 2: charge the current bucket outside the lock (I/O).
	key := fmt.Sprintf("browser-automation:%s:%s:%d", m.userID, m.runID, idx)
	log.Debug().Str("user_id", m.userID).Str("key", key).Msg("browser meter: charging bucket (activate)")
	if err := m.svc.BeforeBrowserIncrement(ctx, m.userID); err != nil {
		m.mu.Lock()
		m.err = err
		m.billingCancel()
		m.mu.Unlock()
		return err
	}
	if err := m.svc.RecordBrowserIncrement(ctx, m.userID, key, m.runID, jobID, durationMS); err != nil {
		m.mu.Lock()
		m.err = err
		m.billingCancel()
		m.mu.Unlock()
		return err
	}

	// Phase 3: mark ever-activated and start the tick goroutine for subsequent buckets.
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return nil
	}
	m.everActivated = true
	tickCtx, cancel := context.WithCancel(ctx)
	m.cancelTick = cancel
	m.mu.Unlock()

	ch, stopTicker := m.newTicker(m.increment)
	go m.runLoop(tickCtx, ch, stopTicker)
	return nil
}

// runLoop fires on each tick and charges the next bucket.
func (m *liveBrowserMeter) runLoop(ctx context.Context, ch <-chan time.Time, stopTicker func()) {
	defer stopTicker()
	for {
		select {
		case <-ch:
			// Prioritise context cancellation when both tick and Done are ready.
			select {
			case <-ctx.Done():
				return
			default:
			}
			m.mu.Lock()
			if m.stopped || m.err != nil {
				m.mu.Unlock()
				return
			}
			idx := m.bucketIdx
			m.bucketIdx++
			jobID := m.jobID
			durationMS := m.increment.Milliseconds()
			billingCancel := m.billingCancel
			m.mu.Unlock()

			key := fmt.Sprintf("browser-automation:%s:%s:%d", m.userID, m.runID, idx)
			log.Debug().Str("user_id", m.userID).Str("key", key).Msg("browser meter: charging bucket (tick)")
			if err := m.svc.BeforeBrowserIncrement(ctx, m.userID); err != nil {
				m.mu.Lock()
				m.err = err
				m.mu.Unlock()
				if billingCancel != nil {
					billingCancel()
				}
				return
			}
			if err := m.svc.RecordBrowserIncrement(ctx, m.userID, key, m.runID, jobID, durationMS); err != nil {
				m.mu.Lock()
				m.err = err
				m.mu.Unlock()
				if billingCancel != nil {
					billingCancel()
				}
				return
			}

		case <-ctx.Done():
			return
		}
	}
}

// Deactivate stops the tick goroutine. The current bucket is non-refundable.
func (m *liveBrowserMeter) Deactivate() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancelTick != nil {
		m.cancelTick()
		m.cancelTick = nil
	}
}

// Err returns a non-nil error after quota exhaustion or a billing failure.
func (m *liveBrowserMeter) Err() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.err
}

// Stop permanently terminates the meter. Subsequent Activate calls are no-ops.
func (m *liveBrowserMeter) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopped = true
	if m.cancelTick != nil {
		m.cancelTick()
		m.cancelTick = nil
	}
	if m.billingCancel != nil {
		m.billingCancel()
		m.billingCancel = nil
	}
}

// BillingCtx returns a context that is cancelled when quota is exhausted or billing
// fails mid-run. Automation may select on this context to avoid destructive actions
// (such as form submit) after the credit balance is depleted. Returns nil before
// the first Activate call.
func (m *liveBrowserMeter) BillingCtx() context.Context {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.billingCtx
}

// SetJobID records the job ID for correlation in billing events.
// Useful for single-application paths; ignored on multi-application runs.
func (m *liveBrowserMeter) SetJobID(jobID string) {
	m.mu.Lock()
	m.jobID = jobID
	m.mu.Unlock()
}

// WasActivated reports whether Activate was ever called successfully.
// Used by waitForSessionRefresh to decide whether to re-activate after a wait.
func (m *liveBrowserMeter) WasActivated() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.everActivated
}
