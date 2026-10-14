package resource

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	resourcedb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/quota"
)

// quotaBurner is the subset of quota.Service needed by the resource billing service.
type quotaBurner interface {
	BeforeCreditUse(ctx context.Context, userID string, credits int64) error
	PrepareTransactionalBurnStrict(userID string, credits int64) (quota.BurnPrepare, error)
	CommitTransactionalBurn(tx *sql.Tx, userID string, credits int64, prep quota.BurnPrepare) (int64, error)
}

// Service meters non-AI resource consumption against the shared credit balance.
type Service struct {
	db    *sql.DB
	store settingsKV
	quota quotaBurner
	// quotaDefaultsFn is the shared economic configuration (ServiceMarkup, CreditsPerUSD).
	quotaDefaultsFn func() domain.QuotaDefaults
}

// NewService constructs a resource billing service.
func NewService(db *sql.DB, store settingsKV, q quotaBurner, quotaDefaults func() domain.QuotaDefaults) *Service {
	return &Service{db: db, store: store, quota: q, quotaDefaultsFn: quotaDefaults}
}

// DB returns the underlying database handle. Intended for testing only.
func (s *Service) DB() *sql.DB { return s.db }

// LoadDefaults returns the current resource metering configuration.
// Fail-closed: returns an error for any storage failure other than "not found".
func (s *Service) LoadDefaults() (domain.ResourceMeteringDefaults, error) {
	return LoadDefaults(s.store)
}

// SaveDefaults validates and persists resource metering configuration.
func (s *Service) SaveDefaults(d domain.ResourceMeteringDefaults) error {
	return SaveDefaults(s.store, d)
}

// EstimateRenderCredits returns the credit cost for one new PDF render at current rates.
// Returns an error if the metering configuration cannot be loaded.
func (s *Service) EstimateRenderCredits() (int64, error) {
	cfg, err := s.LoadDefaults()
	if err != nil {
		return 0, err
	}
	return EstimateRenderCredits(cfg, s.quotaDefaultsFn()), nil
}

// EstimateBrowserIncrementCredits returns the credit cost for one billing increment of
// active browser automation at current rates.
// Returns an error if the metering configuration cannot be loaded.
func (s *Service) EstimateBrowserIncrementCredits() (int64, error) {
	cfg, err := s.LoadDefaults()
	if err != nil {
		return 0, err
	}
	return EstimateBrowserIncrementCredits(cfg, s.quotaDefaultsFn()), nil
}

// Before checks whether a resource operation costing the given credits may proceed.
// Returns quota.ErrExceeded when balance is insufficient.
func (s *Service) Before(ctx context.Context, userID string, credits int64) error {
	if credits <= 0 {
		return nil
	}
	return s.quota.BeforeCreditUse(ctx, userID, credits)
}

// BeforeResource resolves the price for a resource type and performs a preflight balance check.
// Returns (ErrMeteringDisabled, zero ResourcePricing) when metering is off.
// Returns (quota.ErrExceeded, zero ResourcePricing) when the balance is insufficient.
func (s *Service) BeforeResource(ctx context.Context, userID string, rt domain.ResourceType) (ResourcePricing, error) {
	cfg, err := s.LoadDefaults()
	if err != nil {
		return ResourcePricing{}, err
	}
	pricing := PriceResource(cfg, s.quotaDefaultsFn(), rt)
	if pricing.Credits == 0 {
		return ResourcePricing{}, ErrMeteringDisabled
	}
	if err := s.quota.BeforeCreditUse(ctx, userID, pricing.Credits); err != nil {
		return ResourcePricing{}, err
	}
	return pricing, nil
}

// BeforeRender is a convenience preflight for exactly one document_render unit.
// Returns nil when metering is disabled (no-op path). Returns quota.ErrExceeded
// when the balance is insufficient.
func (s *Service) BeforeRender(ctx context.Context, userID string) error {
	_, err := s.BeforeResource(ctx, userID, domain.ResourceTypeDocumentRender)
	if errors.Is(err, ErrMeteringDisabled) {
		return nil
	}
	return err
}

// RecordRender atomically commits one document_render billing event.
// idempotencyKey must be "document-render:<versionID_or_operationID>" to prevent
// double-billing on retries. Duplicate keys are silently ignored (returns nil).
func (s *Service) RecordRender(ctx context.Context, userID, idempotencyKey string) error {
	return s.RecordOnce(ctx, RecordInput{
		UserID:         userID,
		ResourceType:   domain.ResourceTypeDocumentRender,
		IdempotencyKey: idempotencyKey,
		Quantity:       1,
		Unit:           "render",
		Success:        true,
	})
}

// BeforeBrowserIncrement is a preflight for one browser_automation billing increment.
// Returns nil when metering is disabled (no-op path). Returns quota.ErrExceeded
// when the balance is insufficient.
func (s *Service) BeforeBrowserIncrement(ctx context.Context, userID string) error {
	_, err := s.BeforeResource(ctx, userID, domain.ResourceTypeBrowserAutomation)
	if errors.Is(err, ErrMeteringDisabled) {
		return nil
	}
	return err
}

// RecordBrowserIncrement atomically commits one browser_automation billing increment.
// idempotencyKey must be "browser-automation:<userID>:<runID>:<bucketIndex>".
// durationMS is the length of the billed interval (the configured billing period).
// jobID may be empty for multi-application runs.
// Duplicate keys are silently ignored (returns nil).
// Returns nil immediately when browser automation metering is disabled — no event is written.
func (s *Service) RecordBrowserIncrement(ctx context.Context, userID, idempotencyKey, automationRunID, jobID string, durationMS int64) error {
	cfg, err := s.LoadDefaults()
	if err != nil {
		return err
	}
	if !cfg.Enabled || !cfg.Browser.Enabled {
		return nil // true no-op — do not write an event row when metering is off
	}
	return s.RecordOnce(ctx, RecordInput{
		UserID:          userID,
		ResourceType:    domain.ResourceTypeBrowserAutomation,
		IdempotencyKey:  idempotencyKey,
		AutomationRunID: automationRunID,
		JobID:           jobID,
		Quantity:        1,
		Unit:            "increment",
		Success:         true,
		DurationMS:      durationMS,
	})
}

// DefaultsPersisted returns true when an admin has explicitly saved resource-metering
// configuration. Returns (false, nil) when using built-in safe defaults.
func (s *Service) DefaultsPersisted() (bool, error) {
	return DefaultsPersisted(s.store)
}

// Readiness computes the structured resource metering readiness verdict.
// Fail-closed: any storage error yields ready=false.
func (s *Service) Readiness() (domain.ResourceMeteringReadiness, error) {
	cfg, err := s.LoadDefaults()
	if err != nil {
		return domain.ResourceMeteringReadiness{Ready: false, Status: domain.ResourceMeteringStatusInvalid, Warnings: []string{"storage error loading resource metering config"}}, err
	}
	persisted, err := s.DefaultsPersisted()
	if err != nil {
		return domain.ResourceMeteringReadiness{Ready: false, Status: domain.ResourceMeteringStatusInvalid, Warnings: []string{"storage error checking defaults_persisted"}}, err
	}
	qd := s.quotaDefaultsFn()
	return ComputeReadiness(cfg, persisted, qd), nil
}

// BrowserIncrementDuration returns the configured billing increment as a Duration.
// Returns 30 s when metering is disabled or the increment is unset.
func (s *Service) BrowserIncrementDuration() (time.Duration, error) {
	cfg, err := s.LoadDefaults()
	if err != nil {
		return 0, err
	}
	inc := cfg.Browser.BillingIncrementSecs
	if inc <= 0 {
		inc = 30
	}
	return time.Duration(inc) * time.Second, nil
}

// RecordInput describes a resource consumption event to be billed atomically.
// Pricing is computed internally from the current metering configuration —
// do NOT supply cost or credit fields.
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
	Success         bool
	ErrorCode       string
	MetadataJSON    string
}

// RecordOnce atomically writes a resource_usage_events row and burns credits from the user's
// shared balance (trial / monthly / top-up). Idempotent: a duplicate IdempotencyKey is
// silently ignored and returns nil (credits were already burned on the first call).
//
// Pricing is resolved from the current admin configuration; credits_burned in the ledger
// reflects the actual deduction (0 for failed ops, admin-exempt users, and disabled enforcement).
//
// For resource billing, the quota burn is strict: if the balance is insufficient at
// transaction time, returns quota.ErrExceeded without writing any event.
func (s *Service) RecordOnce(ctx context.Context, in RecordInput) error {
	if in.UserID == "" {
		return errors.New("resource billing: missing user_id")
	}
	if in.IdempotencyKey == "" {
		in.IdempotencyKey = uuid.NewString()
	}

	cfg, err := s.LoadDefaults()
	if err != nil {
		log.Ctx(ctx).Error().
			Err(err).
			Str("user_id", in.UserID).
			Str("resource_type", string(in.ResourceType)).
			Msg("resource billing: failed to load metering config")
		return err
	}
	pricing := PriceResource(cfg, s.quotaDefaultsFn(), in.ResourceType)

	var burnCredits int64
	var prep quota.BurnPrepare
	if in.Success && pricing.Credits > 0 {
		burnCredits = pricing.Credits
		prep, err = s.quota.PrepareTransactionalBurnStrict(in.UserID, burnCredits)
		if err != nil {
			level := zerolog.WarnLevel
			msg := "resource billing: quota check failed"
			if errors.Is(err, quota.ErrExceeded) {
				msg = "resource billing: quota exceeded — operation will not be billed"
			} else {
				level = zerolog.ErrorLevel
			}
			log.Ctx(ctx).WithLevel(level).
				Err(err).
				Str("user_id", in.UserID).
				Str("resource_type", string(in.ResourceType)).
				Int64("credits", burnCredits).
				Msg(msg)
			return err
		}
	} else {
		prep = quota.BurnPrepare{SkipBurn: true}
	}

	event := domain.ResourceUsageEvent{
		ID:              uuid.NewString(),
		UserID:          in.UserID,
		ResourceType:    in.ResourceType,
		OperationID:     in.OperationID,
		IdempotencyKey:  in.IdempotencyKey,
		AutomationRunID: in.AutomationRunID,
		ApplicationID:   in.ApplicationID,
		JobID:           in.JobID,
		Quantity:        in.Quantity,
		Unit:            in.Unit,
		DurationMS:      in.DurationMS,
		RawCostUSDMicro:    pricing.RawCostUSDMicro,
		LoadedCostUSDMicro: pricing.LoadedCostUSDMicro,
		Success:      in.Success,
		ErrorCode:    in.ErrorCode,
		MetadataJSON: in.MetadataJSON,
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	quotaUpdate := func(tx *sql.Tx, userID string, credits int64) (int64, error) {
		return s.quota.CommitTransactionalBurn(tx, userID, credits, prep)
	}
	_, err = resourcedb.RecordResourceBillingTx(tx, event, burnCredits, quotaUpdate)
	if err != nil {
		if errors.Is(err, resourcedb.ErrResourceIdempotencyConflict) {
			return nil
		}
		log.Ctx(ctx).Error().
			Err(err).
			Str("user_id", in.UserID).
			Str("resource_type", string(in.ResourceType)).
			Int64("credits", burnCredits).
			Msg("resource billing: failed to record billing event")
		return err
	}
	return tx.Commit()
}
