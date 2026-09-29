package llm

import (
	"context"

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

func (c *Client) recordUsage(ctx context.Context, actualModel string, u *Usage, latencyMS int64, success bool, errCode string, logicalOp bool) {
	if u == nil {
		return
	}
	call := CallContextFrom(ctx)
	if call.UserID == "" {
		call.UserID = c.userID
	}
	if c.tracker != nil {
		c.tracker.AddSync(u, actualModel)
	}
	if c.billing.Ledger == nil {
		if c.tracker == nil {
			log.Error().Str("event", "llm_billing_unconfigured").Msg("llm usage not persisted — no ledger")
		}
		return
	}
	tokens := pricing.TokenUsage{
		InputTokens:  int64(u.InputTokens),
		OutputTokens: int64(u.OutputTokens),
	}
	err := c.billing.Ledger.Record(ctx, usage.RecordInput{
		Call:      call,
		Provider:  c.cfg.Provider,
		Requested: c.cfg.Model,
		Actual:    actualModel,
		Tokens:    tokens,
		LatencyMS: latencyMS,
		Success:   success,
		ErrorCode: errCode,
		LogicalOp: logicalOp,
	})
	if err != nil {
		log.Error().Err(err).Str("event", "llm_billing_record_failed").Str("task", call.Task).Msg("billing record failed")
	}
}
