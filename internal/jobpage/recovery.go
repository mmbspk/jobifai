package jobpage

import (
	"context"
	"fmt"
	"time"
)

// RecoverLocal uses only already-observed reading controls. Exhausting local
// recovery returns the freshest snapshot for semantic interpretation; terminal
// identity, browser and cancellation failures are never sent to the model.
func RecoverLocal(ctx context.Context, page Page, target Target, initial Snapshot, limits Limits) (snapshot Snapshot, result *Result, metrics Metrics, err error) {
	started := time.Now()
	defer func() { metrics.BrowserMS += time.Since(started).Milliseconds() }()
	snapshot = initial
	if err = ValidateSnapshot(snapshot, limits); err != nil {
		return
	}
	scrolled := map[string]bool{}
	waits := 0
	for {
		if err = ctx.Err(); err != nil {
			return
		}
		if plan, planErr := knownPlan(snapshot); planErr == nil {
			var read Result
			read, err = page.Read(ctx, target, snapshot, plan, limits)
			if err == nil {
				read.Source = SourceRecovery
				result = &read
				return
			}
			if terminalRecoveryError(err) {
				return
			}
		}
		if metrics.RecoveryActions >= limits.MaxRecoveryActions {
			err = nil
			return
		}
		var action Action
		// Expansion is reacquired on every loop, including controls replaced by
		// hydration. Application and upload controls are outside this contract.
		for _, allowed := range snapshot.AllowedActions {
			if allowed.Kind == ActionExpand {
				action = allowed
				break
			}
		}
		if action.Kind == "" {
			for i, ref := range snapshot.Candidates {
				n, ok := snapshot.Node(ref)
				if !ok || !n.CanScroll {
					continue
				}
				key := fmt.Sprintf("%s/%s/%s/%d", n.Context, n.Tag, n.Role, i)
				// Bring a thin/empty/lazy section into view once. Full
				// unfamiliar content goes straight to semantic verification.
				if scrolled[key] || (n.Visible && n.TextLength >= 100 && !snapshot.Loading) {
					continue
				}
				scrolled[key] = true
				action = Action{Kind: ActionScroll, Ref: ref, Amount: 500}
				break
			}
		}
		if action.Kind == "" {
			needsWait := snapshot.Loading || len(snapshot.Candidates) == 0
			for _, ref := range snapshot.Candidates {
				n, _ := snapshot.Node(ref)
				needsWait = needsWait || (!n.Truncated && n.TextLength < 100)
			}
			if !needsWait {
				err = nil
				return
			}
			waits++
			amount := 500 * waits
			if amount > 1500 {
				amount = 1500
			}
			action = Action{Kind: ActionWait, Amount: amount}
		}
		metrics.RecoveryActions++
		err = page.Act(ctx, target, snapshot, action)
		if err != nil && ErrorCode(err) != FailureStale {
			return
		}
		snapshot, err = page.Capture(ctx, target, limits)
		if err != nil {
			return
		}
	}
}

func terminalRecoveryError(err error) bool {
	switch ErrorCode(err) {
	case FailureStale, FailureMissing, FailureThin, FailureAmbiguous:
		return false
	default:
		return true
	}
}
