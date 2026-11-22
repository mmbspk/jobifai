package bot

import (
	"context"
	"sync"
	"time"

	"github.com/user/jobifai/internal/domain"
)

type adaptiveBudgetKey struct{}
type adaptiveRunBudget struct {
	mu              sync.Mutex
	calls, maxCalls int
}

// These are attempt limits, independent of account quota and development spend.
// Each batch also uses the configured provider's bounded retry/timeout policy.
func beginAdaptiveRun(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx.Value(adaptiveBudgetKey{}) != nil {
		return ctx, func() {}
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	return context.WithValue(ctx, adaptiveBudgetKey{}, &adaptiveRunBudget{maxCalls: 10}), cancel
}

type boundedFormLLM struct{ delegate StepInterpreterLLM }

func (b *Bot) checkAdaptiveSideEffect() error {
	if b.cfg.BrowserMeter != nil && !DestructiveActionAllowed(b.cfg.BrowserMeter) {
		return b.cfg.BrowserMeter.Err()
	}
	return nil
}
func boundFormLLM(delegate StepInterpreterLLM) StepInterpreterLLM {
	if delegate == nil {
		return nil
	}
	return boundedFormLLM{delegate: delegate}
}
func (b boundedFormLLM) InterpretFields(ctx context.Context, profile *domain.ResumeProfile, snap StepSnapshot, refs []string) ([]LLMFieldProposal, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if budget, ok := ctx.Value(adaptiveBudgetKey{}).(*adaptiveRunBudget); ok {
		budget.mu.Lock()
		if budget.calls >= budget.maxCalls {
			budget.mu.Unlock()
			return nil, ErrLLMLimitExceeded
		}
		budget.calls++
		budget.mu.Unlock()
	}
	return b.delegate.InterpretFields(ctx, profile, snap, refs)
}
