package llm

import (
	"context"
	"errors"
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/user/jobifai/internal/pricing"
	"github.com/user/jobifai/internal/usage"
)

// BillingHooks configures synchronous billing persistence for a client.
type BillingHooks struct {
	Ledger *usage.Ledger
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
	if c.billing.Ledger == nil {
		return nil
	}
	if logicalOp && success && call.OperationID == "" {
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
	return errors.Join(ErrBillingPersistFailed, err)
}
