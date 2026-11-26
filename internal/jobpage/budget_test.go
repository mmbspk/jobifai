package jobpage

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llm"
	"github.com/user/jobifai/internal/pricing"
)

func budgetFixture(limits Limits) (*ModelSession, *stageBudget) {
	s := NewLLMModel(nil, nil, pricing.DefaultCatalog()).NewSession(limits)
	return s, &stageBudget{session: s, stage: "proposal"}
}
func requestFixture(operation string, attempt int) llm.RequestObservation {
	return llm.RequestObservation{Provider: "claude", Model: "claude-haiku-4-5", OperationID: operation, Attempt: attempt, PayloadBytes: 1000, MaxOutputTokens: 100}
}
func usageFixture(operation string, attempt int) llm.UsageObservation {
	return llm.UsageObservation{Call: domain.LLMCallContext{OperationID: operation, Attempt: attempt}, Provider: "claude", RequestedModel: "claude-haiku-4-5", ActualModel: "claude-haiku-4-5", Success: true, Tokens: pricing.TokenUsage{InputTokens: 10, OutputTokens: 5}}
}

func TestJobPageModelBudgetFailedAttemptsRetainReservations(t *testing.T) {
	for name, adjust := range map[string]func(*Limits){
		"tokens": func(l *Limits) { l.MaxTokens = 1500 },
		"cost":   func(l *Limits) { l.MaxCostMicroUSD = 2000 },
	} {
		t.Run(name, func(t *testing.T) {
			limits := DefaultLimits()
			adjust(&limits)
			s, b := budgetFixture(limits)
			require.NoError(t, b.BeforeRequest(context.Background(), requestFixture("op", 1)))
			err := b.BeforeRequest(context.Background(), requestFixture("op", 2))
			require.Equal(t, FailureBudget, ErrorCode(err))
			s.finishOperation()
			require.False(t, s.Metrics().CostKnown)
			require.Equal(t, 1, s.Metrics().HTTPAttempts)
			require.Zero(t, s.Metrics().CostMicroUSD, "reservations are not reported as actual cost")
		})
	}
}

func TestJobPageModelBudgetReportedUsageSettlesReservation(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxTokens = 1500
	s, b := budgetFixture(limits)
	require.NoError(t, b.BeforeRequest(context.Background(), requestFixture("proposal", 1)))
	require.NoError(t, b.ObserveUsage(context.Background(), usageFixture("proposal", 1)))
	v := &stageBudget{session: s, stage: "verification"}
	require.NoError(t, v.BeforeRequest(context.Background(), requestFixture("verification", 1)))
	require.NoError(t, v.ObserveUsage(context.Background(), usageFixture("verification", 1)))
	require.Equal(t, 1, s.Metrics().ProposalRequests)
	require.Equal(t, 1, s.Metrics().VerificationRequests)
	require.EqualValues(t, 30, s.Metrics().InputTokens+s.Metrics().OutputTokens)
	require.Empty(t, s.reserved)
}

func TestJobPageModelBudgetLimitsSharedAcrossStages(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxRequests = 1
	s, b := budgetFixture(limits)
	require.NoError(t, b.BeforeRequest(context.Background(), requestFixture("proposal", 1)))
	require.NoError(t, b.ObserveUsage(context.Background(), usageFixture("proposal", 1)))
	err := (&stageBudget{session: s, stage: "verification"}).BeforeRequest(context.Background(), requestFixture("verification", 1))
	require.Equal(t, FailureBudget, ErrorCode(err))
	require.Equal(t, 1, s.Metrics().HTTPAttempts)
}

func TestJobPageModelBudgetOverageReportsActualUsage(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxTokens = 1500
	s, b := budgetFixture(limits)
	require.NoError(t, b.BeforeRequest(context.Background(), requestFixture("op", 1)))
	u := usageFixture("op", 1)
	u.Tokens.InputTokens = 2000
	err := b.ObserveUsage(context.Background(), u)
	require.Equal(t, FailureBudget, ErrorCode(err))
	require.EqualValues(t, 2000, s.Metrics().InputTokens)
	require.EqualValues(t, 2025, s.Metrics().CostMicroUSD)
	require.Error(t, b.BeforeRequest(context.Background(), requestFixture("new", 1)))
}

func TestJobPageModelBudgetCountsAllCacheTokensAndRejectsOverflow(t *testing.T) {
	s, b := budgetFixture(DefaultLimits())
	require.NoError(t, b.BeforeRequest(context.Background(), requestFixture("op", 1)))
	u := usageFixture("op", 1)
	u.Tokens.CacheWrite5mTokens = 2
	u.Tokens.CacheWrite1hTokens = 3
	u.Tokens.CacheReadTokens = 4
	require.NoError(t, b.ObserveUsage(context.Background(), u))
	require.EqualValues(t, 19, s.Metrics().InputTokens)
	require.EqualValues(t, 43, s.Metrics().CostMicroUSD)
	require.NoError(t, b.BeforeRequest(context.Background(), requestFixture("op2", 1)))
	u = usageFixture("op2", 1)
	u.Tokens.InputTokens = math.MaxInt64
	u.Tokens.CacheReadTokens = 1
	err := b.ObserveUsage(context.Background(), u)
	require.Equal(t, FailureBudget, ErrorCode(err))
	require.False(t, s.Metrics().CostKnown)
	require.Error(t, b.BeforeRequest(context.Background(), requestFixture("op3", 1)), "unaccountable usage blocks further requests")
}
