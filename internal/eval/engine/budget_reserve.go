package engine

import (
	"context"

	"github.com/user/jobifai/internal/eval/candidate"
	"github.com/user/jobifai/internal/eval/dataset"
	"github.com/user/jobifai/internal/eval/tasks"
	"github.com/user/jobifai/internal/pricing"
)

const defaultPerCallReserveMicro int64 = 100_000 // $0.10 conservative fallback

func (s *Service) perCallBudgetReserve(_ context.Context, task string, spec candidate.Spec, c dataset.Case) int64 {
	maxTok := spec.MaxTokens
	if maxTok <= 0 {
		maxTok = 8192
	}
	tok := tasks.ConservativeBillableTokens(task, c, maxTok)
	if s.EvalPricing == nil || s.EvalPricing.Approved == nil {
		return fallbackReserveMicro(tok)
	}
	res, err := s.EvalPricing.Approved.Resolve(spec.Model, false)
	if err != nil {
		return fallbackReserveMicro(tok)
	}
	est := pricing.RawCostMicroUSD(res.Record, tok)
	if est <= 0 {
		return fallbackReserveMicro(tok)
	}
	return est + est/4
}

func fallbackReserveMicro(tok pricing.TokenUsage) int64 {
	micro := pricing.RawCostMicroUSD(pricing.ModelRecord{
		InputPerM: 15, OutputPerM: 75,
	}, tok)
	if micro <= 0 {
		return defaultPerCallReserveMicro
	}
	return micro + micro/4
}
