package resource_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/pricing"
	"github.com/user/jobifai/internal/quota"
	"github.com/user/jobifai/internal/resource"
	"github.com/user/jobifai/internal/usage"
)

// TestConcurrentLedgerAndResourceBilling_NoOverspend verifies that concurrent LLM (usage.Ledger)
// and resource (resource.Service) billing against a near-exhausted trial balance cannot together
// overspend: combined cost (2+3=5) exceeds the 4-credit balance, so exactly one must succeed.
func TestConcurrentLedgerAndResourceBilling_NoOverspend(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	cfg := config.NewStore(db)
	// LLM: PerCallFeeUSD=0.002 → with CreditsPerUSD=1000 → 2 credits per logical op (zero tokens).
	// Resource render: 2000 µ-USD × 1.5 markup × 1000/1_000_000 = 3 credits.
	// Combined: 2+3=5 > 4 (trial balance). Exactly one must commit.
	require.NoError(t, cfg.Set(domain.SystemUserID, "quota_defaults", domain.QuotaDefaults{
		EnforcementDefault: true,
		CreditsPerUSD:      1000,
		ServiceMarkup:      0.5,
		PerCallFeeUSD:      0.002,
		TrialCredits:       4,
		TrialDays:          7,
	}))
	require.NoError(t, cfg.Set(domain.SystemUserID, "resource_metering_defaults", domain.ResourceMeteringDefaults{
		Enabled: true,
		Render:  domain.RenderMeteringConfig{Enabled: true, RawCostUSDMicroPerRender: 2_000},
	}))
	userID := "conc-llm-res-" + t.Name()
	_, err := db.Exec(
		`INSERT INTO users (id, email, password_hash, display_name, is_admin, created_at) VALUES (?, ?, 'x', 'CLR', 0, CURRENT_TIMESTAMP)`,
		userID, userID+"@test.com",
	)
	require.NoError(t, err)
	quotaSvc := quota.NewService(db, cfg, nil)
	require.NoError(t, quotaSvc.InitTrial(context.Background(), userID))

	ledger := &usage.Ledger{
		DB:      db,
		Catalog: pricing.DefaultCatalog(),
		Quota:   quotaSvc,
		Defaults: func() domain.QuotaDefaults {
			return quotaSvc.LoadDefaults()
		},
	}
	resSvc := resource.NewService(db, cfg, quotaSvc, quotaSvc.LoadDefaults)

	var wg sync.WaitGroup
	errs := make([]error, 2)

	// Goroutine 0: LLM billing path via usage.Ledger (the real production path).
	// Zero tokens → cost comes entirely from PerCallFeeUSD (2 credits).
	wg.Add(1)
	go func() {
		defer wg.Done()
		errs[0] = ledger.Record(context.Background(), usage.RecordInput{
			Call: domain.LLMCallContext{
				Task:        domain.TaskJobScoring,
				UserID:      userID,
				OperationID: "conc-llm-op",
			},
			Provider:  "claude",
			Requested: "claude-sonnet-4-6",
			Actual:    "claude-sonnet-4-6",
			Tokens:    pricing.TokenUsage{InputTokens: 0, OutputTokens: 0},
			Success:   true,
			LogicalOp: true,
		})
	}()

	// Goroutine 1: resource billing path via resource.Service (the real production path).
	// Render → 3 credits.
	wg.Add(1)
	go func() {
		defer wg.Done()
		errs[1] = resSvc.RecordOnce(context.Background(), resource.RecordInput{
			UserID:         userID,
			ResourceType:   domain.ResourceTypeDocumentRender,
			IdempotencyKey: "conc-res-op",
			Success:        true,
		})
	}()

	wg.Wait()

	successes := 0
	for _, e := range errs {
		if e == nil {
			successes++
		}
	}
	assert.Equal(t, 1, successes, "exactly one of LLM and resource burns must commit from 4-credit balance (combined=5)")

	row, rowErr := quotaSvc.RowForUser(userID)
	require.NoError(t, rowErr)
	assert.GreaterOrEqual(t, row.TrialRemainingCredits, int64(0), "balance must never go negative")
	// The winner burned either 2 (LLM) or 3 (resource) — so remaining is either 2 or 1.
	assert.LessOrEqual(t, row.TrialRemainingCredits, int64(2), "winner burned at least 2 credits")
}
