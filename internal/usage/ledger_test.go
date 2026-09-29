package usage_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/pricing"
	"github.com/user/jobifai/internal/usage"
)

type stubQuota struct {
	credits int64
}

func (s *stubQuota) RecordLLMBurn(_ context.Context, _ string, credits int64) error {
	s.credits += credits
	return nil
}

func TestLedger_RecordSuccessfulCall(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	sqldb, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })

	q := &stubQuota{}
	ledger := &usage.Ledger{
		DB:      sqldb,
		Catalog: pricing.DefaultCatalog(),
		Quota:   q,
		Defaults: func() domain.QuotaDefaults {
			return domain.QuotaDefaults{CreditsPerUSD: 1000, ServiceMarkup: 0.5, PerCallFeeUSD: 0.002}
		},
	}
	err = ledger.Record(context.Background(), usage.RecordInput{
		Call: domain.LLMCallContext{
			Task:   domain.TaskJobScoring,
			UserID: "user-1",
			JobID:  "job-1",
		},
		Provider:  "claude",
		Requested: "claude-sonnet-4-6",
		Actual:    "claude-sonnet-4-6",
		Tokens:    pricing.TokenUsage{InputTokens: 5000, OutputTokens: 500},
		Success:   true,
		LogicalOp: true,
	})
	require.NoError(t, err)
	require.Greater(t, q.credits, int64(0))

	var n int
	require.NoError(t, sqldb.QueryRow(`SELECT COUNT(*) FROM llm_usage_events WHERE user_id='user-1'`).Scan(&n))
	require.Equal(t, 1, n)
}
