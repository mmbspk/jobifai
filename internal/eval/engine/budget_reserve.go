package engine

import (
	"context"

	"github.com/user/jobifai/internal/eval/candidate"
	"github.com/user/jobifai/internal/pricing"
)

const defaultPerCallReserveMicro int64 = 100_000 // $0.10 conservative fallback

func (s *Service) perCallBudgetReserve(ctx context.Context, spec candidate.Spec) int64 {
	if s.EvalPricing == nil || s.EvalPricing.Approved == nil {
		return defaultPerCallReserveMicro
	}
	res, err := s.EvalPricing.Approved.Resolve(spec.Model, false)
	if err != nil {
		return defaultPerCallReserveMicro
	}
	maxTok := spec.MaxTokens
	if maxTok <= 0 {
		maxTok = 8192
	}
	// Conservative upper bound: typical scoring prompt ~512 in, full max out.
	tok := pricing.TokenUsage{InputTokens: 512, OutputTokens: int64(maxTok)}
	est := pricing.RawCostMicroUSD(res.Record, tok)
	if est <= 0 {
		return defaultPerCallReserveMicro
	}
	// 25% headroom for proxy pricing drift.
	return est + est/4
}
