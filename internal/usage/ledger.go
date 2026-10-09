package usage

import (
	"context"
	"database/sql"
	"errors"
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

// TxQuotaBurner supports quota deduction inside the billing transaction.
type TxQuotaBurner interface {
	PrepareTransactionalBurn(userID string, credits int64) (quota.BurnPrepare, error)
	CommitTransactionalBurn(tx *sql.Tx, userID string, credits int64, prep quota.BurnPrepare) error
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
	Call                domain.LLMCallContext
	Provider            string
	Requested           string
	Actual              string
	ActualModelVerified bool
	Tokens              pricing.TokenUsage
	LatencyMS           int64
	Success             bool
	ErrorCode           string
	LogicalOp           bool // true = successful logical Jobifai operation (one per Chat success)
	PersonalProvider    bool // true when the user's own API key is being used (not the system key)
}

// Record persists billing synchronously in one transaction.
func (l *Ledger) Record(ctx context.Context, in RecordInput) error {
	if l == nil || l.DB == nil {
		return fmt.Errorf("usage ledger not configured")
	}
	userID := in.Call.UserID
	if userID == "" {
		return fmt.Errorf("billing: missing user_id")
	}
	if in.LogicalOp && in.Call.OperationID == "" {
		return fmt.Errorf("billing: missing operation id for billable call")
	}

	localOllama := in.Provider == "ollama"
	pricingSource := "local/ollama"
	pricingVersion := "local"
	var rawMicro int64
	if localOllama {
		rawMicro = 0
	} else {
		res, err := l.Catalog.Resolve(in.Actual, false)
		if err != nil {
			return err
		}
		if res.UsedFallback {
			log.Warn().
				Str("event", "llm_unpriced_model").
				Str("model", in.Actual).
				Str("task", in.Call.Task).
				Msg("billing used conservative fallback pricing for unknown model")
		}
		rawMicro = pricing.RawCostMicroUSD(res.Record, in.Tokens)
		pricingSource = res.Source
		_, ver, refreshed := l.Catalog.Meta()
		pricingVersion = ver
		if !refreshed.IsZero() {
			pricingVersion = ver + "@" + refreshed.Format(time.RFC3339)
		}
	}
	def := domain.QuotaDefaults{}
	if l.Defaults != nil {
		def = l.Defaults()
	}
	loadedMicro := int64(0)
	credits := int64(0)
	if !localOllama {
		loadedUSD := quota.USDFromMicro(rawMicro) * (1 + def.ServiceMarkup)
		if in.LogicalOp && in.Success && def.PerCallFeeUSD > 0 {
			loadedUSD += def.PerCallFeeUSD
		}
		if loadedUSD < 0 {
			loadedUSD = 0
		}
		loadedMicro = int64(loadedUSD * 1_000_000)
		credits = quota.CreditsFromLoadedUSD(def, loadedMicro)
		if credits < 1 && in.Success && in.LogicalOp && (rawMicro > 0 || def.PerCallFeeUSD > 0) {
			credits = 1
		}
	}
	if !in.Success || !in.LogicalOp {
		credits = 0
		if localOllama {
			loadedMicro = 0
		} else {
			loadedMicro = 0
		}
	}

	idempotencyKey := ""
	if in.LogicalOp && in.Call.OperationID != "" {
		idempotencyKey = in.Call.OperationID
		if !in.Success {
			idempotencyKey += ":failed"
		}
	}

	event := db.LLMUsageEventInput{
		UserID:              userID,
		Task:                domain.LegacyToStableTask(in.Call.Task),
		Provider:            in.Provider,
		RequestedModel:      in.Requested,
		ActualModel:         in.Actual,
		ActualModelVerified: in.ActualModelVerified,
		InputTokens:         in.Tokens.InputTokens,
		OutputTokens:        in.Tokens.OutputTokens,
		CacheWrite5mTokens:  in.Tokens.CacheWrite5mTokens,
		CacheWrite1hTokens:  in.Tokens.CacheWrite1hTokens,
		CacheReadTokens:     in.Tokens.CacheReadTokens,
		RawCostUSDMicro:     rawMicro,
		LoadedCostUSDMicro:  loadedMicro,
		CreditsBurned:       credits,
		LatencyMS:           in.LatencyMS,
		Success:             in.Success,
		LogicalOp:           in.LogicalOp,
		ErrorCode:           in.ErrorCode,
		CorrelationID:       in.Call.CorrelationID,
		IdempotencyKey:      idempotencyKey,
		JobID:               in.Call.JobID,
		ApplicationID:       in.Call.ApplicationID,
		AutomationRunID:     in.Call.AutomationRunID,
		Attempt:             in.Call.Attempt,
		PricingSource:       pricingSource,
		PricingVersion:      pricingVersion,
		PersonalProvider:    in.PersonalProvider,
	}

	var burnPrep quota.BurnPrepare
	if credits > 0 && l.Quota != nil {
		tb, ok := l.Quota.(TxQuotaBurner)
		if !ok {
			return fmt.Errorf("quota service does not support transactional burn")
		}
		var prepErr error
		burnPrep, prepErr = tb.PrepareTransactionalBurn(userID, credits)
		if prepErr != nil {
			return prepErr
		}
	}

	tx, err := l.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var quotaFn func(*sql.Tx, string, int64) error
	if credits > 0 && l.Quota != nil {
		tb := l.Quota.(TxQuotaBurner)
		prep := burnPrep
		quotaFn = func(tx *sql.Tx, uid string, c int64) error {
			return tb.CommitTransactionalBurn(tx, uid, c, prep)
		}
	}

	_, err = db.RecordBillingTx(tx, event, credits, quotaFn)
	if errors.Is(err, db.ErrIdempotencyConflict) {
		if commitErr := tx.Commit(); commitErr != nil {
			return commitErr
		}
		return nil
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}
