package jobpage

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/user/jobifai/internal/llm"
	"github.com/user/jobifai/internal/pricing"
)

type reservation struct{ tokens, cost int64 }

// ModelSession is scoped to one read. Failed/uncertain HTTP requests retain
// their reservation, while reported successful usage settles it. Metrics count
// actual attempts and reported usage, rather than reservation estimates.
type ModelSession struct {
	model           *LLMModel
	limits          Limits
	started         time.Time
	callMu          sync.Mutex
	mu              sync.Mutex
	metrics         Metrics
	reserved        map[string]reservation
	committedTokens int64
	committedCost   int64
	accountingErr   error
}

func (s *ModelSession) Metrics() Metrics {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.metrics
}

func (s *ModelSession) finishOperation() {
	s.mu.Lock()
	defer s.mu.Unlock()
	// A transport/decoding failure may still have consumed provider usage.
	// Keep its reservation and do not present the known subtotal as final cost.
	if len(s.reserved) > 0 {
		s.metrics.CostKnown = false
	}
}

func modelBudgetError(reason string) error {
	return &ReadError{Code: FailureBudget, Cause: fmt.Errorf("%s", reason)}
}

// Called under mu. An unaccountable response cannot authorize another request.
func (s *ModelSession) rejectAccounting(reason string) error {
	s.metrics.CostKnown = false
	s.accountingErr = modelBudgetError(reason)
	return s.accountingErr
}

func (s *ModelSession) callContext(ctx context.Context) (context.Context, context.CancelFunc, error) {
	if s.limits.WallClockSeconds <= 0 || s.limits.MaxRequests <= 0 || s.limits.MaxTokens <= 0 || s.limits.MaxCostMicroUSD <= 0 {
		return nil, nil, modelBudgetError("invalid model limits")
	}
	ctx, cancel := context.WithDeadline(ctx, s.started.Add(time.Duration(s.limits.WallClockSeconds)*time.Second))
	if err := ctx.Err(); err != nil {
		cancel()
		return nil, nil, err
	}
	return ctx, cancel, nil
}

type stageBudget struct {
	session *ModelSession
	stage   string
}

func reservationKey(operation string, attempt int) string {
	return fmt.Sprintf("%s/%d", operation, attempt)
}

func (b *stageBudget) BeforeRequest(ctx context.Context, o llm.RequestObservation) error {
	s := b.session
	if err := ctx.Err(); err != nil {
		return err
	}
	if time.Since(s.started) >= time.Duration(s.limits.WallClockSeconds)*time.Second {
		return modelBudgetError("model deadline exceeded")
	}
	if o.PayloadBytes <= 0 || o.MaxOutputTokens <= 0 || o.OperationID == "" {
		return modelBudgetError("request accounting unavailable")
	}
	if s.model.catalog == nil {
		return modelBudgetError("pricing catalogue unavailable")
	}
	price, err := s.model.catalog.Resolve(o.Model, true)
	if err != nil || !price.Known || price.UsedFallback {
		return modelBudgetError("configured model pricing unresolved")
	}
	if price.Record.Provider != o.Provider && price.Record.Provider != "legacy-prefix" {
		return modelBudgetError("configured provider pricing mismatch")
	}
	// The generic client provides a conservative native-image bound when
	// needed; text/framing uses one token per serialized UTF-8 byte.
	input := o.InputTokenUpperBound
	if input == 0 {
		input = int64(o.PayloadBytes)
	}
	if input < 0 || input > math.MaxInt64-int64(o.MaxOutputTokens) {
		return modelBudgetError("invalid input token bound")
	}
	tokens := input + int64(o.MaxOutputTokens)
	cost := pricing.RawCostMicroUSD(price.Record, pricing.TokenUsage{InputTokens: input, OutputTokens: int64(o.MaxOutputTokens)})
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.accountingErr != nil {
		return s.accountingErr
	}
	reservedTokens, reservedCost, err := s.reservedUsage()
	if err != nil {
		return err
	}
	if s.committedTokens < 0 || s.committedCost < 0 || s.committedTokens > s.limits.MaxTokens || reservedTokens > s.limits.MaxTokens-s.committedTokens || s.committedCost > s.limits.MaxCostMicroUSD || reservedCost > s.limits.MaxCostMicroUSD-s.committedCost {
		return s.rejectAccounting("committed model usage exceeded limit")
	}
	if s.metrics.HTTPAttempts >= s.limits.MaxRequests || tokens > s.limits.MaxTokens-s.committedTokens-reservedTokens || cost > s.limits.MaxCostMicroUSD-s.committedCost-reservedCost {
		return modelBudgetError("request, token or cost limit exceeded")
	}
	key := reservationKey(o.OperationID, o.Attempt)
	if _, exists := s.reserved[key]; exists {
		return modelBudgetError("duplicate request accounting key")
	}
	s.reserved[key] = reservation{tokens: tokens, cost: cost}
	s.metrics.HTTPAttempts++
	if o.Attempt <= 1 {
		if b.stage == "proposal" {
			s.metrics.ProposalRequests++
		} else {
			s.metrics.VerificationRequests++
		}
	}
	return nil
}

func (b *stageBudget) ObserveUsage(_ context.Context, o llm.UsageObservation) error {
	s := b.session
	s.mu.Lock()
	defer s.mu.Unlock()
	in := int64(0)
	for _, count := range []int64{o.Tokens.InputTokens, o.Tokens.CacheReadTokens, o.Tokens.CacheWrite5mTokens, o.Tokens.CacheWrite1hTokens} {
		if count < 0 || count > math.MaxInt64-in {
			return s.rejectAccounting("provider usage accounting invalid")
		}
		in += count
	}
	out := o.Tokens.OutputTokens
	if out < 0 || out > math.MaxInt64-in || in+out <= 0 || in > math.MaxInt64-s.metrics.InputTokens || out > math.MaxInt64-s.metrics.OutputTokens {
		return s.rejectAccounting("provider usage accounting unavailable")
	}
	s.metrics.InputTokens += in
	s.metrics.OutputTokens += out
	key := reservationKey(o.Call.OperationID, o.Call.Attempt)
	if _, ok := s.reserved[key]; !ok {
		return s.rejectAccounting("usage has no request reservation")
	}
	price, err := s.model.catalog.Resolve(pricing.CatalogLookupKey(o.RequestedModel, o.ActualModel), true)
	if err != nil || !price.Known || price.UsedFallback || (price.Record.Provider != o.Provider && price.Record.Provider != "legacy-prefix") {
		return s.rejectAccounting("actual model pricing unresolved")
	}
	cost := pricing.RawCostMicroUSD(price.Record, o.Tokens)
	if cost < 0 || cost > math.MaxInt64-s.metrics.CostMicroUSD {
		return s.rejectAccounting("provider cost accounting invalid")
	}
	s.metrics.CostMicroUSD += cost
	if in+out > math.MaxInt64-s.committedTokens || cost > math.MaxInt64-s.committedCost {
		return s.rejectAccounting("committed model accounting overflow")
	}
	s.committedTokens += in + out
	s.committedCost += cost
	delete(s.reserved, key)
	reservedTokens, reservedCost, err := s.reservedUsage()
	if err != nil {
		return err
	}
	if s.committedTokens > s.limits.MaxTokens || reservedTokens > s.limits.MaxTokens-s.committedTokens || s.committedCost > s.limits.MaxCostMicroUSD || reservedCost > s.limits.MaxCostMicroUSD-s.committedCost {
		return s.rejectAccounting("reported model usage exceeded limit")
	}
	return nil
}

// Called under mu; even corrupted accounting must not wrap signed headroom.
func (s *ModelSession) reservedUsage() (tokens, cost int64, err error) {
	for _, entry := range s.reserved {
		if entry.tokens < 0 || entry.cost < 0 || entry.tokens > math.MaxInt64-tokens || entry.cost > math.MaxInt64-cost {
			return 0, 0, s.rejectAccounting("reserved model accounting overflow")
		}
		tokens += entry.tokens
		cost += entry.cost
	}
	return tokens, cost, nil
}
