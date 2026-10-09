package llm

import (
	"context"
	"errors"
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/pricing"
	"github.com/user/jobifai/internal/usage"
)

// UsageObservation is emitted for eval/admin calls that must not hit customer billing.
type UsageObservation struct {
	Call                domain.LLMCallContext
	Provider            string
	RequestedModel      string
	ActualModel         string
	ActualModelVerified bool
	ActualModelRaw      string
	CanonicalPricingModel string
	PricingSource       string
	PricingKind         pricing.LookupKind
	PricingResolved     bool
	PricingError        string
	Tokens              pricing.TokenUsage
	LatencyMS           int64
	Success             bool
	ErrorCode           string
	RawCostMicro        int64
	BudgetChargeMicro   int64
}

// BillingHooks configures synchronous billing persistence for a client.
type BillingHooks struct {
	Ledger       *usage.Ledger
	Catalog      *pricing.Catalog         // optional; used for eval cost when Ledger is nil
	EvalPrice    func(ctx context.Context, provider, requested, actual string) (pricing.LookupResult, pricing.EvalPricingMeta, error)
	EvalObserver func(UsageObservation) // optional; never writes llm_usage_events
}

func (c *Client) WithBilling(h BillingHooks) *Client {
	c.billing = h
	return c
}

type billingUsage struct {
	pricing.TokenUsage
	ActualModel         string
	ActualModelVerified bool
}

func (c *Client) recordUsage(ctx context.Context, bu billingUsage, latencyMS int64, success bool, errCode string, logicalOp bool) error {
	call := CallContextFrom(ctx)
	if call.UserID == "" {
		call.UserID = c.userID
	}
	if c.tracker != nil && success {
		c.tracker.AddSync(&Usage{
			InputTokens:  int(bu.InputTokens),
			OutputTokens: int(bu.OutputTokens),
		}, bu.ActualModel)
	}
	if c.billing.EvalObserver != nil {
		rawMicro := int64(0)
		meta := pricing.EvalPricingMeta{
			RequestedModel: c.cfg.Model,
			ActualModelRaw: bu.ActualModel,
		}
		if c.billing.EvalPrice != nil {
			res, pm, pErr := c.billing.EvalPrice(ctx, c.cfg.Provider, c.cfg.Model, bu.ActualModel)
			meta = pm
			if pErr == nil && pm.Resolved {
				rawMicro = pricing.RawCostMicroUSD(res.Record, bu.TokenUsage)
			} else if pErr != nil && meta.ResolveError == "" {
				meta.ResolveError = pErr.Error()
			}
		} else {
			cat := c.billing.Catalog
			if cat == nil && c.billing.Ledger != nil {
				cat = c.billing.Ledger.Catalog
			}
			if cat != nil {
				key := pricing.CatalogLookupKey(c.cfg.Model, bu.ActualModel)
				meta.CanonicalPricingModel = key
				res, err := cat.Resolve(key, true)
				if err == nil && res.Known && !res.UsedFallback {
					rawMicro = pricing.RawCostMicroUSD(res.Record, bu.TokenUsage)
					meta.Source = res.Source
					meta.Kind = res.Kind
					meta.Resolved = true
				} else if err != nil {
					meta.ResolveError = err.Error()
				}
			}
		}
		budgetMicro := rawMicro
		if success && bu.InputTokens+bu.OutputTokens > 0 {
			if !meta.Resolved {
				log.Warn().Str("requested_model", c.cfg.Model).Str("actual_model", bu.ActualModel).
					Str("task", call.Task).Msg("eval pricing unresolved for successful llm call")
				budgetMicro = evalBudgetChargeFallback(c, ctx, bu.TokenUsage, meta)
			}
		}
		c.billing.EvalObserver(UsageObservation{
			Call: call, Provider: c.cfg.Provider, RequestedModel: c.cfg.Model,
			ActualModel: bu.ActualModel, ActualModelVerified: bu.ActualModelVerified,
			ActualModelRaw: bu.ActualModel, CanonicalPricingModel: meta.CanonicalPricingModel,
			PricingSource: meta.Source, PricingKind: meta.Kind, PricingResolved: meta.Resolved,
			PricingError: meta.ResolveError,
			Tokens: bu.TokenUsage, LatencyMS: latencyMS, Success: success, ErrorCode: errCode,
			RawCostMicro: rawMicro, BudgetChargeMicro: budgetMicro,
		})
	}
	if c.billing.Ledger == nil {
		return nil
	}
	if logicalOp && call.OperationID == "" {
		return fmt.Errorf("billing: missing operation id")
	}
	if call.UserID == "" && logicalOp && success {
		return fmt.Errorf("billing: missing user attribution")
	}
	err := c.billing.Ledger.Record(ctx, usage.RecordInput{
		Call:                call,
		Provider:            c.cfg.Provider,
		Requested:           c.cfg.Model,
		Actual:              bu.ActualModel,
		ActualModelVerified: bu.ActualModelVerified,
		Tokens:              bu.TokenUsage,
		LatencyMS:           latencyMS,
		Success:             success,
		ErrorCode:           errCode,
		LogicalOp:           logicalOp,
		PersonalProvider:    c.personalProvider,
	})
	if err != nil {
		log.Error().Err(err).Str("event", "llm_billing_record_failed").Str("task", call.Task).Msg("billing record failed")
		return err
	}
	return nil
}

func billingPersistErr(err error) error {
	if err == nil {
		return nil
	}
	return &nonRetryableError{err: errors.Join(ErrBillingPersistFailed, err)}
}

func evalBudgetChargeFallback(c *Client, ctx context.Context, tok pricing.TokenUsage, meta pricing.EvalPricingMeta) int64 {
	if c.billing.EvalPrice != nil {
		res, pm, err := c.billing.EvalPrice(ctx, c.cfg.Provider, c.cfg.Model, c.cfg.Model)
		if err == nil && pm.Resolved {
			return pricing.RawCostMicroUSD(res.Record, tok)
		}
	}
	cat := c.billing.Catalog
	if cat == nil && c.billing.Ledger != nil {
		cat = c.billing.Ledger.Catalog
	}
	if cat != nil {
		res, err := cat.Resolve(c.cfg.Model, false)
		if err == nil {
			est := pricing.RawCostMicroUSD(res.Record, tok)
			if est > 0 {
				return est
			}
		}
	}
	if cat != nil {
		res, _ := cat.Resolve(c.cfg.Model, false)
		return pricing.RawCostMicroUSD(res.Record, tok)
	}
	_ = meta
	return 0
}
