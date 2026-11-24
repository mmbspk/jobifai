package bot

// step_exec.go — Bounded step executor for the adaptive apply engine (story #132).
//
// StepExecutor drives the execution of a StepInterpretation against a live form.
// It owns the retry/timeout/no-progress bounds and the crash-safe submission
// protocol (persist-before-click).
//
// The actual browser interactions are delegated to a FieldFiller interface so
// the bounds logic can be tested without Rod.
//
// Security note: submit intent is persisted atomically BEFORE the click. A
// crash between persist and click produces outcome=submission_unknown, which
// the reconciler handles. Never submit without a durable intent record.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ── bounds ────────────────────────────────────────────────────────────────────

// StepExecBounds controls the execution budget for one form step.
type StepExecBounds struct {
	// MaxFillAttempts is the maximum number of times to retry filling one field
	// before giving up and marking it as a fail.
	MaxFillAttempts int
	// MaxStepDuration caps the total wall-clock time for one step (fill + verify).
	MaxStepDuration time.Duration
	// MaxNoProgressSteps is the number of consecutive steps where no field was
	// successfully filled before the executor aborts (no-progress guard).
	MaxNoProgressSteps int
}

// DefaultStepExecBounds returns safe defaults for production use.
func DefaultStepExecBounds() StepExecBounds {
	return StepExecBounds{
		MaxFillAttempts:    3,
		MaxStepDuration:    90 * time.Second,
		MaxNoProgressSteps: 3,
	}
}

// ── field filler interface ─────────────────────────────────────────────────────

// FieldFillResult is the outcome of filling one form field.
type FieldFillResult struct {
	Ref     string
	Filled  bool  // true when the action was executed without error
	Skipped bool  // true when the action was ActionSkip
	Err     error // non-nil if the fill action failed (not a skip)
}

// FieldFiller is the browser interaction contract for the step executor.
// Implementations are Rod-based; the interface enables testing without a browser.
type FieldFiller interface {
	// Fill executes one FieldResolution against the current page state.
	// Callers must pass a fresh SnapField for the ref from the current snapshot
	// (not a stale ref from a prior render pass).
	Fill(ctx context.Context, f SnapField, res FieldResolution) FieldFillResult

	// NavigateNext advances the form to the next page/step.
	// Returns true when the navigation succeeded, false when no next button was found.
	NavigateNext(ctx context.Context) (bool, error)

	// IsSubmitReady returns true when the submit button is visible and enabled.
	IsSubmitReady(ctx context.Context) (bool, error)

	// ClickSubmit clicks the submit button exactly once.
	// The caller must have called SubmitGuard.PersistIntent before invoking this.
	ClickSubmit(ctx context.Context) error
}

// submitIntentFiller lets native fillers finish hover, identity and document
// checks before persisting intent, close to their one mouse press. Final guards
// still recheck after the durable write, which can wait on database contention.
// Once persistIntent succeeds, errors remain submission_unknown; intent is never
// cleared based on a browser error.
type submitIntentFiller interface {
	ClickSubmitWithIntent(ctx context.Context, persistIntent func() error) error
}

// ── submit guard ──────────────────────────────────────────────────────────────

// SubmitGuard enforces the crash-safe submission protocol.
// PersistIntent MUST succeed before ClickSubmit is called; any crash between
// persist and click is recoverable as outcome=submission_unknown.
type SubmitGuard struct {
	DB        *sql.DB
	AttemptID string
	UserID    string
}

// PersistIntent atomically records that the submit button is about to be clicked.
// Returns ErrNoActiveAttempt if the attempt row is not in-progress (duplicate worker
// or cancelled before submission reached).
func (g *SubmitGuard) PersistIntent() error {
	if g == nil || g.DB == nil {
		return nil
	}
	return markAttemptSubmitDispatch(g.DB, g.AttemptID, g.UserID)
}

// ErrNoActiveAttempt is returned when the attempt row cannot be found or is not
// in an in-progress state; indicates a duplicate worker or external cancellation.
var ErrNoActiveAttempt = errors.New("step_exec: attempt not active — possible duplicate worker")

// ── step execution result ──────────────────────────────────────────────────────

// StepExecOutcome describes what the executor decided to do with the step.
type StepExecOutcome string

const (
	// StepOutcomeAdvanced: all non-pending fields were filled, form navigated to next step.
	StepOutcomeAdvanced StepExecOutcome = "advanced"
	// StepOutcomePaused: one or more fields are pending — step could not be completed.
	StepOutcomePaused StepExecOutcome = "paused"
	// StepOutcomeSubmitted: submit button was clicked after crash-safe intent persist.
	StepOutcomeSubmitted StepExecOutcome = "submitted"
	// StepOutcomeTimeout: exceeded MaxStepDuration.
	StepOutcomeTimeout StepExecOutcome = "timeout"
	// StepOutcomeNoProgress: exceeded MaxNoProgressSteps with no field filled.
	StepOutcomeNoProgress StepExecOutcome = "no_progress"
	// StepOutcomeError: unrecoverable error from browser or quota layer.
	StepOutcomeError StepExecOutcome = "error"
)

// StepExecResult summarises what happened during one step execution.
type StepExecResult struct {
	Outcome      StepExecOutcome
	FieldsFilled int      // fields successfully written
	FilledRefs   []string // refs the filler reported Filled:true for
	FieldsFailed []string // refs that failed even after retries
	FieldsPended []string // refs that could not be resolved (pending)
	NoProgressAt int      // step index where no-progress was detected (0 if none)
	Err          error    // non-nil for StepOutcomeError
}

// ── executor ──────────────────────────────────────────────────────────────────

// StepExecutor runs the bounded execution loop for one form step.
type StepExecutor struct {
	Bounds     StepExecBounds
	Guard      *SubmitGuard
	NoProgress *noProgressTracker
	// BeforeSideEffect rechecks attempt policy at every fill, navigation and submit.
	BeforeSideEffect func(context.Context) error
}

// NewStepExecutor creates a StepExecutor with the given bounds and submit guard.
func NewStepExecutor(bounds StepExecBounds, guard *SubmitGuard) *StepExecutor {
	return &StepExecutor{
		Bounds:     bounds,
		Guard:      guard,
		NoProgress: &noProgressTracker{max: bounds.MaxNoProgressSteps},
	}
}

// ExecuteStep fills all non-pending fields in interp, navigates the form, and
// handles the submit path when the final step is reached.
//
// snap must be freshly captured from the current page — do not reuse a snapshot
// from a prior call.
func (se *StepExecutor) ExecuteStep(ctx context.Context, snap StepSnapshot, interp StepInterpretation, filler FieldFiller) (StepExecResult, error) {
	deadline := time.Now().Add(se.Bounds.MaxStepDuration)
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	result := StepExecResult{}

	// Collect pending refs first.
	for _, r := range interp.Fields {
		if r.Pending {
			result.FieldsPended = append(result.FieldsPended, r.Ref)
		}
	}

	// If the interpretation is paused, we cannot proceed.
	if interp.Paused {
		result.Outcome = StepOutcomePaused
		return result, nil
	}

	// Fill each non-pending, non-filled field.
	filledThisStep := 0
	attemptedThisStep := 0
	var fillErrors []error
	for _, res := range interp.Fields {
		if res.Pending {
			continue
		}
		if res.Action == ActionSkip {
			continue
		}

		f, ok := snap.FieldByRef(res.Ref)
		if !ok {
			result.FieldsFailed = append(result.FieldsFailed, res.Ref)
			continue
		}

		attemptedThisStep++
		var fillResult FieldFillResult
		for attempt := 0; attempt < se.Bounds.MaxFillAttempts; attempt++ {
			if ctx.Err() != nil {
				result.Outcome = StepOutcomeTimeout
				return result, ctx.Err()
			}
			if se.BeforeSideEffect != nil {
				if err := se.BeforeSideEffect(ctx); err != nil {
					result.Outcome, result.Err = StepOutcomeError, err
					return result, err
				}
			}
			fillResult = filler.Fill(ctx, f, res)
			if errors.Is(fillResult.Err, errAdaptiveDocumentReview) {
				result.FieldsFailed = append(result.FieldsFailed, res.Ref)
				result.Outcome, result.Err = StepOutcomeError, fillResult.Err
				return result, result.Err
			}
			if fillResult.Filled || fillResult.Skipped {
				break
			}
			if errors.Is(fillResult.Err, errAdaptiveQuestionReview) || errors.Is(fillResult.Err, errAdaptiveDocumentReview) {
				break
			}
		}

		if fillResult.Filled {
			filledThisStep++
			result.FieldsFilled++
			result.FilledRefs = append(result.FilledRefs, res.Ref)
		} else if !fillResult.Skipped {
			result.FieldsFailed = append(result.FieldsFailed, res.Ref)
			if fillResult.Err != nil {
				fillErrors = append(fillErrors, fillResult.Err)
			}
		}
	}

	// Hard fields failed: surface as error.
	if len(result.FieldsFailed) > 0 {
		result.Outcome = StepOutcomeError
		result.Err = errors.Join(append(fillErrors, fmt.Errorf("step_exec: %d field(s) could not be filled: %v", len(result.FieldsFailed), result.FieldsFailed))...)
		return result, result.Err
	}

	// No-progress check: only fires when fills were attempted but none succeeded.
	if attemptedThisStep > 0 && filledThisStep == 0 {
		if se.NoProgress.increment() {
			result.Outcome = StepOutcomeNoProgress
			result.NoProgressAt = se.NoProgress.count
			return result, fmt.Errorf("step_exec: no progress after %d consecutive steps", se.NoProgress.count)
		}
	} else {
		se.NoProgress.reset()
	}

	// Check if submit button is ready.
	if se.BeforeSideEffect != nil {
		if err := se.BeforeSideEffect(ctx); err != nil {
			result.Outcome, result.Err = StepOutcomeError, err
			return result, err
		}
	}
	if filler != nil {
		ready, err := filler.IsSubmitReady(ctx)
		if err != nil {
			result.Outcome = StepOutcomeError
			result.Err = err
			return result, err
		}
		if ready {
			intentPersisted := false
			persistIntent := func() error {
				if intentPersisted {
					return ErrNoActiveAttempt
				}
				if err := ctx.Err(); err != nil {
					return err
				}
				if err := se.Guard.PersistIntent(); err != nil {
					return fmt.Errorf("step_exec: failed to persist submit intent: %w", err)
				}
				intentPersisted = true
				return nil
			}
			var clickErr error
			if native, ok := filler.(submitIntentFiller); ok {
				clickErr = native.ClickSubmitWithIntent(ctx, persistIntent)
			} else {
				// Other fillers retain the original persist-before-click contract.
				clickErr = persistIntent()
				if clickErr == nil {
					clickErr = filler.ClickSubmit(ctx)
				}
			}
			if !intentPersisted {
				if clickErr == nil {
					clickErr = errors.New("step_exec: submit filler did not persist intent")
				}
				result.Outcome, result.Err = StepOutcomeError, clickErr
				return result, clickErr
			}
			if clickErr != nil {
				// Intent was persisted; outcome is submission_unknown, not failed.
				result.Outcome = StepOutcomeSubmitted
				result.Err = fmt.Errorf("step_exec: submit click error (intent persisted, outcome unknown): %w", clickErr)
				return result, nil
			}
			result.Outcome = StepOutcomeSubmitted
			return result, nil
		}
	}

	// Navigate to next step.
	advanced, err := filler.NavigateNext(ctx)
	if err != nil {
		result.Outcome = StepOutcomeError
		result.Err = err
		return result, err
	}
	if !advanced {
		result.Outcome = StepOutcomeError
		result.Err = errors.New("step_exec: could not advance form — no next button found")
		return result, result.Err
	}

	result.Outcome = StepOutcomeAdvanced
	return result, nil
}

// ── no-progress tracker ───────────────────────────────────────────────────────

type noProgressTracker struct {
	count int
	max   int
}

// increment records a no-progress step and returns true when the limit is exceeded.
func (t *noProgressTracker) increment() bool {
	t.count++
	return t.count >= t.max
}

func (t *noProgressTracker) reset() {
	t.count = 0
}
