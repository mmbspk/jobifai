package resource

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	resourcedb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/quota"
)

// quotaBurner is the subset of quota.Service needed by the resource billing service.
// quota.Service satisfies this interface.
type quotaBurner interface {
	BeforeCreditUse(ctx context.Context, userID string, credits int64) error
	PrepareTransactionalBurn(userID string, credits int64) (quota.BurnPrepare, error)
	CommitTransactionalBurn(tx *sql.Tx, userID string, credits int64, prep quota.BurnPrepare) error
}

// Service meters non-AI resource consumption against the shared credit balance.
type Service struct {
	db    *sql.DB
	store settingsKV
	quota quotaBurner
	// quotaDefaults is the shared economic configuration (ServiceMarkup, CreditsPerUSD).
	quotaDefaults func() domain.QuotaDefaults
}

// NewService constructs a resource billing service.
func NewService(db *sql.DB, store settingsKV, q quotaBurner, quotaDefaults func() domain.QuotaDefaults) *Service {
	return &Service{db: db, store: store, quota: q, quotaDefaults: quotaDefaults}
}

// LoadDefaults returns the current resource metering configuration.
func (s *Service) LoadDefaults() domain.ResourceMeteringDefaults {
	return LoadDefaults(s.store)
}

// SaveDefaults validates and persists resource metering configuration.
func (s *Service) SaveDefaults(d domain.ResourceMeteringDefaults) error {
	return SaveDefaults(s.store, d)
}

// EstimateRenderCredits returns the credit cost for one new PDF render at current rates.
func (s *Service) EstimateRenderCredits() int64 {
	return EstimateRenderCredits(s.LoadDefaults(), s.quotaDefaults())
}

// EstimateBrowserIncrementCredits returns the credit cost for one billing increment of
// active browser automation at current rates.
func (s *Service) EstimateBrowserIncrementCredits() int64 {
	return EstimateBrowserIncrementCredits(s.LoadDefaults(), s.quotaDefaults())
}

// Before checks whether a resource operation costing the given credits may proceed.
// Returns ErrMeteringDisabled when metering is off (caller may treat as no-op).
// Returns quota.ErrExceeded when balance is insufficient.
func (s *Service) Before(ctx context.Context, userID string, credits int64) error {
	if credits <= 0 {
		return nil
	}
	return s.quota.BeforeCreditUse(ctx, userID, credits)
}

// RecordInput describes a resource consumption event to be billed atomically.
type RecordInput struct {
	UserID          string
	ResourceType    domain.ResourceType
	OperationID     string
	IdempotencyKey  string
	AutomationRunID string
	ApplicationID   string
	JobID           string
	Quantity        float64
	Unit            string
	DurationMS      int64
	// RawCostUSDMicro and LoadedCostUSDMicro are provided by the caller after computing
	// with EstimateCredits / rate helpers. CreditsBurned must match.
	RawCostUSDMicro    int64
	LoadedCostUSDMicro int64
	CreditsBurned      int64
	Success            bool
	ErrorCode          string
	MetadataJSON       string
}

// RecordOnce atomically writes a resource_usage_events row and burns credits from the user's
// shared balance (trial / monthly / top-up). Idempotent: a duplicate IdempotencyKey is
// silently ignored and returns nil (credits were already burned on the first call).
//
// No credits are burned for failed operations (Success=false) or disabled meters (CreditsBurned=0).
func (s *Service) RecordOnce(ctx context.Context, in RecordInput) error {
	if in.UserID == "" {
		return errors.New("resource billing: missing user_id")
	}
	if in.IdempotencyKey == "" {
		in.IdempotencyKey = uuid.NewString()
	}

	credits := in.CreditsBurned
	prep, err := s.quota.PrepareTransactionalBurn(in.UserID, credits)
	if err != nil {
		return err
	}

	event := domain.ResourceUsageEvent{
		ID:                 uuid.NewString(),
		UserID:             in.UserID,
		ResourceType:       in.ResourceType,
		OperationID:        in.OperationID,
		IdempotencyKey:     in.IdempotencyKey,
		AutomationRunID:    in.AutomationRunID,
		ApplicationID:      in.ApplicationID,
		JobID:              in.JobID,
		Quantity:           in.Quantity,
		Unit:               in.Unit,
		DurationMS:         in.DurationMS,
		RawCostUSDMicro:    in.RawCostUSDMicro,
		LoadedCostUSDMicro: in.LoadedCostUSDMicro,
		CreditsBurned:      in.CreditsBurned,
		Success:            in.Success,
		ErrorCode:          in.ErrorCode,
		MetadataJSON:       in.MetadataJSON,
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	quotaUpdate := func(tx *sql.Tx, userID string, burnCredits int64) error {
		return s.quota.CommitTransactionalBurn(tx, userID, burnCredits, prep)
	}
	_, err = resourcedb.RecordResourceBillingTx(tx, event, credits, quotaUpdate)
	if err != nil {
		if errors.Is(err, resourcedb.ErrResourceIdempotencyConflict) {
			// Already recorded — commit the empty tx and return nil.
			_ = tx.Commit()
			return nil
		}
		return err
	}
	return tx.Commit()
}
