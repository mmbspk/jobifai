package usage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/pricing"
	"github.com/user/jobifai/internal/quota"
)

// QuotaRecorder applies credit burns after a successful billed call.
type QuotaRecorder interface {
	RecordLLMBurn(ctx context.Context, userID string, credits int64) error
}

// Ledger persists LLM usage events and keeps legacy aggregates in sync.
type Ledger struct {
	DB       *sql.DB
	Catalog  *pricing.Catalog
	Quota    QuotaRecorder
	Defaults func() domain.QuotaDefaults
}

// RecordInput is everything needed to bill one provider response.
type RecordInput struct {
	Call       domain.LLMCallContext
	Provider   string
	Requested  string
	Actual     string
	Tokens     pricing.TokenUsage
	LatencyMS  int64
	Success    bool
	ErrorCode  string
	LogicalOp  bool // true = successful logical Jobifai operation (one per Chat success)
}

// Record persists billing synchronously. Failures are logged and returned — callers must not ignore.
func (l *Ledger) Record(ctx context.Context, in RecordInput) error {
	if l == nil || l.DB == nil {
		return fmt.Errorf("usage ledger not configured")
	}
	userID := in.Call.UserID
	if userID == "" {
		userID = in.Call.UserID
	}
	rec, priceSrc, _ := l.Catalog.Lookup(in.Actual)
	rawMicro := pricing.RawCostMicroUSD(rec, in.Tokens)
	def := domain.QuotaDefaults{}
	if l.Defaults != nil {
		def = l.Defaults()
	}
	loadedUSD := quota.USDFromMicro(rawMicro)*(1+def.ServiceMarkup) + 0.0
	if in.LogicalOp && in.Success && def.PerCallFeeUSD > 0 {
		loadedUSD += def.PerCallFeeUSD
	}
	if loadedUSD < 0 {
		loadedUSD = 0
	}
	loadedMicro := int64(loadedUSD * 1_000_000)
	credits := quota.CreditsFromLoadedUSD(def, loadedMicro)
	if credits < 1 && in.Success && in.LogicalOp && (rawMicro > 0 || def.PerCallFeeUSD > 0) {
		credits = 1
	}
	if !in.Success || !in.LogicalOp {
		credits = 0
		loadedMicro = 0
	}

	_, priceVer, refreshed := l.Catalog.Meta()
	pricingVersion := priceVer
	if !refreshed.IsZero() {
		pricingVersion = priceVer + "@" + refreshed.Format(time.RFC3339)
	}
	event := db.LLMUsageEventInput{
		UserID:             userID,
		Task:               domain.LegacyToStableTask(in.Call.Task),
		Provider:           in.Provider,
		RequestedModel:     in.Requested,
		ActualModel:        in.Actual,
		InputTokens:        in.Tokens.InputTokens,
		OutputTokens:       in.Tokens.OutputTokens,
		CacheWriteTokens:   in.Tokens.CacheWriteTokens,
		CacheReadTokens:    in.Tokens.CacheReadTokens,
		RawCostUSDMicro:    rawMicro,
		LoadedCostUSDMicro: loadedMicro,
		CreditsBurned:      credits,
		LatencyMS:          in.LatencyMS,
		Success:            in.Success,
		ErrorCode:          in.ErrorCode,
		CorrelationID:      in.Call.CorrelationID,
		JobID:              in.Call.JobID,
		ApplicationID:      in.Call.ApplicationID,
		AutomationRunID:    in.Call.AutomationRunID,
		Attempt:            in.Call.Attempt,
		PricingSource:      priceSrc,
		PricingVersion:     pricingVersion,
	}
	if _, err := db.InsertLLMUsageEvent(l.DB, event); err != nil {
		log.Error().Err(err).Str("event", "llm_billing_persist_failed").Str("task", event.Task).Msg("failed to persist llm usage event")
		return err
	}

	if in.Success && in.LogicalOp && (in.Tokens.InputTokens > 0 || in.Tokens.OutputTokens > 0) {
		if err := db.IncrementUsage(l.DB, userID, in.Actual, in.Tokens.InputTokens, in.Tokens.OutputTokens, 1); err != nil {
			log.Error().Err(err).Msg("increment usage_totals failed")
			return err
		}
	}
	if in.Success && in.LogicalOp && credits > 0 && l.Quota != nil {
		if err := l.Quota.RecordLLMBurn(ctx, userID, credits); err != nil {
			log.Error().Err(err).Int64("credits", credits).Msg("quota burn failed")
			return err
		}
	}
	return nil
}
