package usage_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/config"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/pricing"
	"github.com/user/jobifai/internal/quota"
	"github.com/user/jobifai/internal/usage"
)

func loadTestQuotaDefaults(cfg *config.Store) domain.QuotaDefaults {
	var d domain.QuotaDefaults
	if err := cfg.Get(domain.SystemUserID, quota.KeyQuotaDefaults, &d); err != nil {
		return domain.QuotaDefaults{CreditsPerUSD: 1000, ServiceMarkup: 0.5, PerCallFeeUSD: 0.002}
	}
	return d
}

func TestLedger_RecordWithRealQuota_NoDeadlock(t *testing.T) {
	sqldb, err := appdb.Open(filepath.Join(t.TempDir(), "ledger-int.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })

	cfg := config.NewStore(sqldb)
	require.NoError(t, cfg.Set(domain.SystemUserID, quota.KeyQuotaDefaults, domain.QuotaDefaults{
		EnforcementDefault: true,
		CreditsPerUSD:      1000,
		ServiceMarkup:      0.5,
		PerCallFeeUSD:      0.002,
		TrialCredits:       500,
		TrialDays:          7,
	}))

	users := auth.NewUserStore(sqldb)
	u, err := users.Create("ledger-int@example.com", "hash", "Ledger")
	require.NoError(t, err)

	qSvc := quota.NewService(sqldb, cfg, users)
	require.NoError(t, qSvc.InitTrial(context.Background(), u.ID))

	ledger := &usage.Ledger{
		DB:      sqldb,
		Catalog: pricing.DefaultCatalog(),
		Quota:   qSvc,
		Defaults: func() domain.QuotaDefaults {
			return loadTestQuotaDefaults(cfg)
		},
	}

	rowBefore, err := qSvc.RowForUser(u.ID)
	require.NoError(t, err)
	trialBefore := rowBefore.TrialRemainingCredits

	done := make(chan error, 1)
	go func() {
		done <- ledger.Record(context.Background(), usage.RecordInput{
			Call: domain.LLMCallContext{
				Task:        domain.TaskJobScoring,
				UserID:      u.ID,
				OperationID: "op-integration-1",
			},
			Provider:  "claude",
			Requested: "claude-sonnet-4-6",
			Actual:    "claude-sonnet-4-6",
			Tokens:    pricing.TokenUsage{InputTokens: 5000, OutputTokens: 500},
			Success:   true,
			LogicalOp: true,
		})
	}()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("ledger.Record deadlocked")
	}

	var events int
	require.NoError(t, sqldb.QueryRow(`SELECT COUNT(*) FROM llm_usage_events WHERE user_id=?`, u.ID).Scan(&events))
	assert.Equal(t, 1, events)

	var inTok int64
	require.NoError(t, sqldb.QueryRow(`SELECT input_tokens FROM usage_totals WHERE user_id=? AND model=?`, u.ID, "claude-sonnet-4-6").Scan(&inTok))
	assert.Equal(t, int64(5000), inTok)

	rowAfter, err := qSvc.RowForUser(u.ID)
	require.NoError(t, err)
	assert.Less(t, rowAfter.TrialRemainingCredits, trialBefore)
}

func TestLedger_RecordWithRealQuota_TrialOverrideNotReplenished(t *testing.T) {
	sqldb, err := appdb.Open(filepath.Join(t.TempDir(), "ledger-trial.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })

	cfg := config.NewStore(sqldb)
	require.NoError(t, cfg.Set(domain.SystemUserID, quota.KeyQuotaDefaults, domain.QuotaDefaults{
		EnforcementDefault: true,
		CreditsPerUSD:      1000,
		ServiceMarkup:      0,
		PerCallFeeUSD:      0,
		TrialCredits:       500,
		TrialDays:          7,
	}))

	users := auth.NewUserStore(sqldb)
	u, err := users.Create("trial-ledger@example.com", "hash", "Trial")
	require.NoError(t, err)

	qSvc := quota.NewService(sqldb, cfg, users)
	require.NoError(t, qSvc.InitTrial(context.Background(), u.ID))
	override := 1000
	require.NoError(t, qSvc.SaveUserOverrides(u.ID, domain.QuotaUserOverrides{TrialCredits: &override}))

	ledger := &usage.Ledger{
		DB:      sqldb,
		Catalog: pricing.DefaultCatalog(),
		Quota:   qSvc,
		Defaults: func() domain.QuotaDefaults {
			return loadTestQuotaDefaults(cfg)
		},
	}

	record := func(opID string) {
		t.Helper()
		require.NoError(t, ledger.Record(context.Background(), usage.RecordInput{
			Call: domain.LLMCallContext{
				Task:        domain.TaskJobScoring,
				UserID:      u.ID,
				OperationID: opID,
			},
			Provider:  "claude",
			Requested: "claude-sonnet-4-6",
			Actual:    "claude-sonnet-4-6",
			Tokens:    pricing.TokenUsage{InputTokens: 5000, OutputTokens: 500},
			Success:   true,
			LogicalOp: true,
		}))
	}

	row, err := qSvc.RowForUser(u.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(1000), row.TrialRemainingCredits)
	record("op-t1")
	row, err = qSvc.RowForUser(u.ID)
	require.NoError(t, err)
	afterFirst := row.TrialRemainingCredits
	assert.Less(t, afterFirst, int64(1000))
	record("op-t2")
	row, err = qSvc.RowForUser(u.ID)
	require.NoError(t, err)
	assert.Less(t, row.TrialRemainingCredits, afterFirst)
}

func TestLedger_RecordWithRealQuota_AdminSkipsBurn(t *testing.T) {
	sqldb, err := appdb.Open(filepath.Join(t.TempDir(), "ledger-admin.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })

	cfg := config.NewStore(sqldb)
	require.NoError(t, cfg.Set(domain.SystemUserID, quota.KeyQuotaDefaults, domain.QuotaDefaults{
		EnforcementDefault: true,
		CreditsPerUSD:      1000,
		TrialCredits:       500,
	}))

	users := auth.NewUserStore(sqldb)
	admin, err := users.Create("admin@example.com", "hash", "Admin")
	require.NoError(t, err)
	require.NoError(t, users.SetAdmin(admin.ID, true))
	require.NoError(t, quota.NewService(sqldb, cfg, users).InitTrial(context.Background(), admin.ID))

	qSvc := quota.NewService(sqldb, cfg, users)
	ledger := &usage.Ledger{
		DB:      sqldb,
		Catalog: pricing.DefaultCatalog(),
		Quota:   qSvc,
		Defaults: func() domain.QuotaDefaults {
			return loadTestQuotaDefaults(cfg)
		},
	}

	require.NoError(t, ledger.Record(context.Background(), usage.RecordInput{
		Call: domain.LLMCallContext{
			Task:        domain.TaskJobScoring,
			UserID:      admin.ID,
			OperationID: "op-admin-1",
		},
		Provider:  "claude",
		Requested: "claude-sonnet-4-6",
		Actual:    "claude-sonnet-4-6",
		Tokens:    pricing.TokenUsage{InputTokens: 1000, OutputTokens: 100},
		Success:   true,
		LogicalOp: true,
	}))

	row, err := qSvc.RowForUser(admin.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(500), row.TrialRemainingCredits)
}
