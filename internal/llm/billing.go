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
	Tokens              pricing.TokenUsage
	LatencyMS           int64
	Success             bool
	ErrorCode           string
	RawCostMicro        int64
}

// BillingHooks configures synchronous billing persistence for a client.
type BillingHooks struct {
	Ledger       *usage.Ledger
	Catalog      *pricing.Catalog         // optional; used for eval cost when Ledger is nil
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
		cat := c.billing.Catalog
		if cat == nil && c.billing.Ledger != nil {
			cat = c.billing.Ledger.Catalog
		}
		if cat != nil {
			if res, err := cat.Resolve(bu.ActualModel, false); err == nil {
				rawMicro = pricing.RawCostMicroUSD(res.Record, bu.TokenUsage)
			}
		}
		c.billing.EvalObserver(UsageObservation{
			Call: call, Provider: c.cfg.Provider, RequestedModel: c.cfg.Model,
			ActualModel: bu.ActualModel, ActualModelVerified: bu.ActualModelVerified,
			Tokens: bu.TokenUsage, LatencyMS: latencyMS, Success: success, ErrorCode: errCode,
			RawCostMicro: rawMicro,
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
