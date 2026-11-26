package jobpage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Reader owns the common resolution order. Each interpretation session belongs
// to one read; only structural recipes can be shared across listings.
type Reader struct {
	Store    RecipeStore
	Config   func(context.Context) (Config, error)
	NewModel func(context.Context, Limits) (Model, error)
	mu       sync.Mutex
	flights  map[string]*readFlight
}
type readFlight struct {
	token chan struct{}
	users int
}

func NewReader(store RecipeStore, config func(context.Context) (Config, error), model func(context.Context, Limits) (Model, error)) *Reader {
	return &Reader{Store: store, Config: config, NewModel: model, flights: map[string]*readFlight{}}
}

func (r *Reader) configuration(ctx context.Context) (Config, error) {
	if r.Config == nil {
		return Config{Recovery: ModeOff, Reuse: ModeOff, Limits: DefaultLimits()}, nil
	}
	c, e := r.Config(ctx)
	if e != nil {
		return c, e
	}
	return NormalizeConfig(c)
}
func enabled(mode Mode, users []string, user string) bool {
	if mode == ModeOn {
		return true
	}
	if mode != ModePilot || user == "" {
		return false
	}
	for _, id := range users {
		if id == user {
			return true
		}
	}
	return false
}
func (r *Reader) enabled(ctx context.Context, t Target, reuse bool) (bool, error) {
	c, err := r.configuration(ctx)
	if err != nil {
		return false, &ReadError{Code: FailurePersistence, Cause: err}
	}
	mode := c.Recovery
	if reuse {
		mode = c.Reuse
	}
	return enabled(mode, c.PilotUsers, t.UserID), nil
}

func (r *Reader) Read(ctx context.Context, page Page, target Target) (out Result, err error) {
	start := time.Now()
	metrics := Metrics{CostKnown: true}
	defer func() {
		out.Target = target
		out.Metrics = metrics
		out.Metrics.LatencyMS = time.Since(start).Milliseconds()
	}()
	target, _, err = NormalizeTarget(target)
	if err != nil {
		return out, err
	}
	cfg, e := r.configuration(ctx)
	if e != nil {
		return out, &ReadError{Code: FailurePersistence, Cause: e}
	}
	limits := cfg.Limits
	if limits.WallClockSeconds <= 0 {
		return out, &ReadError{Code: FailureBudget}
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(limits.WallClockSeconds)*time.Second)
	defer cancel()
	timed := func(fn func() error) error {
		s := time.Now()
		e := fn()
		metrics.BrowserMS += time.Since(s).Milliseconds()
		return e
	}
	metrics.DeterministicReads++
	var known Result
	knownErr := timed(func() error { var e error; known, e = page.Known(ctx, target, limits); return e })
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	if knownErr == nil {
		knownErr = ValidateResult(target, known, limits)
		if knownErr == nil {
			known.Source = SourceDeterministic
			return known, nil
		}
	}
	if terminalReadError(knownErr) {
		return out, knownErr
	}
	recovery, reuse := enabled(cfg.Recovery, cfg.PilotUsers, target.UserID), enabled(cfg.Reuse, cfg.PilotUsers, target.UserID)
	if !recovery && !reuse {
		return out, knownErr
	}
	var snap Snapshot
	if e = timed(func() error { var e error; snap, e = page.Capture(ctx, target, limits); return e }); e != nil {
		return out, e
	}
	if e = ValidateSnapshot(snap, limits); e != nil {
		return out, e
	}
	out.LayoutSignature = snap.LayoutSignature
	replay := func() (Result, bool, error) {
		on, e := r.enabled(ctx, target, true)
		if e != nil {
			return Result{}, false, e
		}
		if !on || r.Store == nil {
			return Result{}, false, nil
		}
		recipes, e := r.Store.Lookup(ctx, snap.Scope, snap.LayoutSignature)
		if e != nil {
			return Result{}, false, &ReadError{Code: FailurePersistence, Cause: e}
		}
		for _, recipe := range recipes {
			if ctx.Err() != nil {
				return Result{}, false, ctx.Err()
			}
			plan, pe := PlanFromRecipe(snap, recipe, limits)
			var result Result
			if pe == nil {
				pe = timed(func() error { var e error; result, e = page.Read(ctx, target, snap, plan, limits); return e })
			}
			if ErrorCode(pe) == FailureStale {
				// A waiting reader or hydration may replace otherwise compatible
				// nodes. Rebind fresh refs once before judging reusable structure.
				e := timed(func() error { var e error; snap, e = page.Capture(ctx, target, limits); return e })
				if e != nil {
					return Result{}, false, e
				}
				if e = ValidateSnapshot(snap, limits); e != nil {
					return Result{}, false, e
				}
				plan, pe = PlanFromRecipe(snap, recipe, limits)
				if pe == nil {
					pe = timed(func() error { var e error; result, e = page.Read(ctx, target, snap, plan, limits); return e })
				}
			}
			if pe == nil {
				pe = ValidateResult(target, result, limits)
			}
			if pe == nil {
				on, e := r.enabled(ctx, target, true)
				if e != nil {
					return Result{}, false, e
				}
				if !on {
					return Result{}, false, nil
				}
				metrics.RecipeHits++
				result.Source = SourceRecipe
				result.RecipeID = recipe.ID
				result.RecipeVersion = recipe.Version
				return result, true, nil
			}
			if ctx.Err() != nil {
				return Result{}, false, ctx.Err()
			}
			if terminalReadError(pe) {
				return Result{}, false, pe
			}
			if ErrorCode(pe) == FailureStale || ErrorCode(pe) == FailureThin {
				// Transient rendering and capture exhaustion do not disprove a
				// globally reusable method. Recovery receives the fresh observation.
				return Result{}, false, nil
			}
			if e := r.Store.Invalidate(ctx, recipe.ID, recipeInvalidationReason(pe)); e != nil {
				return Result{}, false, &ReadError{Code: FailurePersistence, Cause: e}
			}
		}
		return Result{}, false, nil
	}
	if result, ok, e := replay(); e != nil {
		return out, e
	} else if ok {
		return result, nil
	}
	if recovery {
		var local *Result
		guardedPage := &recoveryGuard{Page: page, before: func(actionCtx context.Context) error {
			on, e := r.enabled(actionCtx, target, false)
			if e != nil {
				return e
			}
			if !on {
				return errRecoveryDisabled
			}
			return nil
		}}
		e = timed(func() error {
			var e error
			snap, local, _, e = RecoverLocal(ctx, guardedPage, target, snap, limits)
			return e
		})
		metrics.RecoveryActions += guardedPage.attempts
		if errors.Is(e, errRecoveryDisabled) {
			return out, knownErr
		}
		if e != nil {
			return out, e
		}
		if local != nil {
			if e = ValidateResult(target, *local, limits); e == nil {
				on, e := r.enabled(ctx, target, false)
				if e != nil {
					return out, e
				}
				if !on {
					return out, knownErr
				}
				local.Source = SourceRecovery
				return *local, nil
			}
		}
		if e = ValidateSnapshot(snap, limits); e != nil {
			return out, e
		}
	}
	// Serialise compatible cold misses. Followers replay their own page after
	// the leader learns; description text and results are never shared.
	release, e := r.acquire(ctx, fmt.Sprint(snap.Scope)+"/"+snap.LayoutSignature)
	if e != nil {
		return out, e
	}
	defer release()
	if result, ok, e := replay(); e != nil {
		return out, e
	} else if ok {
		return result, nil
	}
	on, e := r.enabled(ctx, target, false)
	if e != nil {
		return out, e
	}
	if !on {
		return out, knownErr
	}
	if len(snap.Candidates) == 0 {
		return out, &ReadError{Code: FailureMissing}
	}
	if snap.Loading {
		return out, &ReadError{Code: FailureThin, Cause: errors.New("listing content is still loading")}
	}
	if r.NewModel == nil {
		return out, &ReadError{Code: FailureProvider, Cause: errors.New("job-page model unavailable")}
	}
	model, e := r.NewModel(ctx, limits)
	if e == nil && model == nil {
		e = &ReadError{Code: FailureProvider, Cause: errors.New("job-page model unavailable")}
	}
	if e != nil {
		return out, modelFailure(e)
	}
	defer func() {
		if observed, ok := model.(interface{ Metrics() Metrics }); ok {
			m := observed.Metrics()
			metrics.ProposalRequests = m.ProposalRequests
			metrics.VerificationRequests = m.VerificationRequests
			metrics.HTTPAttempts = m.HTTPAttempts
			metrics.InputTokens = m.InputTokens
			metrics.OutputTokens = m.OutputTokens
			metrics.CostMicroUSD = m.CostMicroUSD
			metrics.CostKnown = m.CostKnown
		}
	}()
	logicalRequests := 0
	for round := 0; round < limits.MaxRequests; round++ {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		on, e = r.enabled(ctx, target, false)
		if e != nil {
			return out, e
		}
		if !on {
			return out, knownErr
		}
		if logicalRequests >= limits.MaxRequests {
			return out, &ReadError{Code: FailureBudget}
		}
		logicalRequests++
		if vision, ok := model.(interface{ SupportsScreenshot() bool }); ok && vision.SupportsScreenshot() {
			if capture, ok := page.(interface {
				ScreenshotFor(context.Context, Target, Snapshot, Limits) ([]byte, error)
			}); ok {
				e := timed(func() error {
					var e error
					snap.Screenshot, e = capture.ScreenshotFor(ctx, target, snap, limits)
					return e
				})
				if e != nil {
					return out, e
				}
			}
		}
		plan, e := model.Propose(ctx, snap)
		if e != nil {
			return out, modelFailure(e)
		}
		if e = ValidatePlan(snap, plan, limits); e != nil {
			return out, e
		}
		if len(plan.Actions) > 0 {
			if metrics.RecoveryActions >= limits.MaxRecoveryActions {
				return out, &ReadError{Code: FailureBudget}
			}
			// Every action can rerender. Execute one, recapture, and request a fresh
			// plan instead of applying old refs to replacement nodes.
			on, e = r.enabled(ctx, target, false)
			if e != nil {
				return out, e
			}
			if !on {
				return out, knownErr
			}
			e = timed(func() error { return page.Act(ctx, target, snap, plan.Actions[0]) })
			if e != nil {
				return out, e
			}
			metrics.RecoveryActions++
			e = timed(func() error { var e error; snap, e = page.Capture(ctx, target, limits); return e })
			if e != nil {
				return out, e
			}
			if e = ValidateSnapshot(snap, limits); e != nil {
				return out, e
			}
			continue
		}
		on, e = r.enabled(ctx, target, false)
		if e != nil {
			return out, e
		}
		if !on {
			return out, knownErr
		}
		if logicalRequests >= limits.MaxRequests {
			return out, &ReadError{Code: FailureBudget}
		}
		logicalRequests++
		verification, e := model.Verify(ctx, snap, plan)
		if e != nil {
			return out, modelFailure(e)
		}
		if e = ValidateVerification(snap, plan, verification); e != nil {
			return out, e
		}
		var result Result
		e = timed(func() error { var e error; result, e = page.Read(ctx, target, snap, plan, limits); return e })
		if ErrorCode(e) == FailureStale {
			e = timed(func() error { var e error; snap, e = page.Capture(ctx, target, limits); return e })
			if e != nil {
				return out, e
			}
			if e = ValidateSnapshot(snap, limits); e != nil {
				return out, e
			}
			continue
		}
		if e != nil {
			return out, e
		}
		if e = ValidateResult(target, result, limits); e != nil {
			return out, e
		}
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		on, e = r.enabled(ctx, target, false)
		if e != nil {
			return out, e
		}
		if !on {
			return out, knownErr
		}
		result.Source = SourceModel
		result.LayoutSignature = snap.LayoutSignature
		// The live selection proof is mandatory even when recipe storage is
		// disabled. Storage availability cannot weaken acceptance of content.
		recipe, e := BuildVerifiedRecipe(ctx, snap, plan, result, verification, limits)
		if e != nil {
			if errors.Is(e, ErrNoSafeRecipe) {
				return result, nil
			}
			return out, e
		}
		if r.Store != nil {
			recipe, e = r.Store.SaveVerified(ctx, recipe)
			if errors.Is(e, ErrInactiveRecipe) {
				return result, nil
			}
			if e != nil {
				return out, &ReadError{Code: FailurePersistence, Cause: e}
			}
			result.RecipeID, result.RecipeVersion = recipe.ID, recipe.Version
		}
		return result, nil
	}
	return out, &ReadError{Code: FailureBudget}
}

var errRecoveryDisabled = errors.New("page recovery disabled during read")

func modelFailure(err error) error {
	if err != nil && ErrorCode(err) == "" {
		return &ReadError{Code: FailureProvider, Cause: err}
	}
	return err
}

func recipeInvalidationReason(err error) string {
	switch ErrorCode(err) {
	case FailureAmbiguous:
		return "ambiguous"
	case FailureStale:
		return "stale_snapshot"
	case FailureThin, FailureMissing:
		return "incomplete"
	default:
		return "verification_failed"
	}
}

type recoveryGuard struct {
	Page
	before   func(context.Context) error
	attempts int
}

func (p *recoveryGuard) Act(ctx context.Context, target Target, snapshot Snapshot, action Action) error {
	if err := p.before(ctx); err != nil {
		return err
	}
	p.attempts++
	return p.Page.Act(ctx, target, snapshot, action)
}

func terminalReadError(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	switch ErrorCode(err) {
	case FailureIdentity, FailureDrift, FailureOrigin, FailureAuth, FailureExpired, FailureBrowser, FailureBudget, FailurePersistence, FailureProvider, FailureQuota:
		return true
	}
	return false
}
func ValidateResult(t Target, result Result, limits Limits) error {
	current, scope, e := NormalizeTarget(result.Target)
	if e != nil {
		return e
	}
	_, want, e := NormalizeTarget(t)
	if e != nil {
		return e
	}
	if current.JobID != t.JobID || scope != want || !result.Identity.Verified || result.Identity.JobID != t.JobID || (t.Title != "" && normalizeText(t.Title) != normalizeText(result.Identity.Title)) || (t.Company != "" && normalizeText(t.Company) != normalizeText(result.Identity.Company)) {
		return &ReadError{Code: FailureIdentity}
	}
	length := utf8.RuneCountInString(strings.TrimSpace(result.Description))
	if length < 100 {
		return &ReadError{Code: FailureThin}
	}
	if length > limits.MaxDescriptionChars {
		return &ReadError{Code: FailureBudget}
	}
	return nil
}
func (r *Reader) acquire(ctx context.Context, key string) (func(), error) {
	r.mu.Lock()
	if r.flights == nil {
		r.flights = map[string]*readFlight{}
	}
	flight := r.flights[key]
	if flight == nil {
		flight = &readFlight{token: make(chan struct{}, 1)}
		r.flights[key] = flight
	}
	flight.users++
	r.mu.Unlock()
	cleanup := func() {
		r.mu.Lock()
		flight.users--
		if flight.users == 0 {
			delete(r.flights, key)
		}
		r.mu.Unlock()
	}
	select {
	case flight.token <- struct{}{}:
		return func() { <-flight.token; cleanup() }, nil
	case <-ctx.Done():
		cleanup()
		return nil, ctx.Err()
	}
}
