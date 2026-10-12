package usage_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/pricing"
	"github.com/user/jobifai/internal/quota"
	"github.com/user/jobifai/internal/usage"
)

type stubQuota struct {
	credits int64
}

func (s *stubQuota) RecordLLMBurn(_ context.Context, _ string, credits int64) error {
	s.credits += credits
	return nil
}

func (s *stubQuota) PrepareTransactionalBurnStrict(_ string, credits int64) (quota.BurnPrepare, error) {
	if credits <= 0 {
		return quota.BurnPrepare{SkipBurn: true}, nil
	}
	return quota.BurnPrepare{}, nil
}

func (s *stubQuota) CommitTransactionalBurn(_ *sql.Tx, _ string, credits int64, prep quota.BurnPrepare) (int64, error) {
	if prep.SkipBurn {
		return 0, nil
	}
	s.credits += credits
	return credits, nil
}

func TestLedger_RecordSuccessfulCall(t *testing.T) {
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
			Task:        domain.TaskJobScoring,
			UserID:      "user-1",
			JobID:       "job-1",
			OperationID: "op-1",
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

func TestLedger_PropagatesRunAndJobIDs(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	sqldb, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })

	ledger := &usage.Ledger{
		DB:      sqldb,
		Catalog: pricing.DefaultCatalog(),
		Quota:   &stubQuota{},
	}
	err = ledger.Record(context.Background(), usage.RecordInput{
		Call: domain.LLMCallContext{
			Task:            domain.TaskJobScoring,
			UserID:          "user-1",
			JobID:           "job-99",
			AutomationRunID: "run-abc",
			OperationID:     "op-99",
		},
		Provider:  "claude",
		Requested: "claude-sonnet-4-6",
		Actual:    "claude-sonnet-4-6",
		Tokens:    pricing.TokenUsage{InputTokens: 100, OutputTokens: 10},
		Success:   true,
		LogicalOp: true,
	})
	require.NoError(t, err)

	var jobID, runID string
	require.NoError(t, sqldb.QueryRow(`SELECT COALESCE(job_id,''), COALESCE(automation_run_id,'') FROM llm_usage_events LIMIT 1`).Scan(&jobID, &runID))
	require.Equal(t, "job-99", jobID)
	require.Equal(t, "run-abc", runID)
}

func TestLedger_IdempotentOperationID(t *testing.T) {
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
	in := usage.RecordInput{
		Call: domain.LLMCallContext{
			Task:        domain.TaskJobScoring,
			UserID:      "user-1",
			OperationID: "op-dedupe",
		},
		Provider:  "claude",
		Requested: "claude-sonnet-4-6",
		Actual:    "claude-sonnet-4-6",
		Tokens:    pricing.TokenUsage{InputTokens: 100, OutputTokens: 10},
		Success:   true,
		LogicalOp: true,
	}
	require.NoError(t, ledger.Record(context.Background(), in))
	creditsAfterFirst := q.credits
	require.NoError(t, ledger.Record(context.Background(), in))
	require.Equal(t, creditsAfterFirst, q.credits)

	var n int
	require.NoError(t, sqldb.QueryRow(`SELECT COUNT(*) FROM llm_usage_events WHERE user_id='user-1'`).Scan(&n))
	require.Equal(t, 1, n)
}

// TestLedger_ConcurrentSameOperationID_NoDuplicate verifies that when multiple goroutines
// attempt to record the same OperationID concurrently, all return nil (idempotent) and
// exactly one event is stored with exactly one credit burn.
func TestLedger_ConcurrentSameOperationID_NoDuplicate(t *testing.T) {
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
	in := usage.RecordInput{
		Call: domain.LLMCallContext{
			Task:        domain.TaskJobScoring,
			UserID:      "conc-user",
			OperationID: "op-concurrent",
		},
		Provider:  "claude",
		Requested: "claude-sonnet-4-6",
		Actual:    "claude-sonnet-4-6",
		Tokens:    pricing.TokenUsage{InputTokens: 100, OutputTokens: 10},
		Success:   true,
		LogicalOp: true,
	}

	const goroutines = 3
	errs := make([]error, goroutines)
	var wg sync.WaitGroup
	for i := range goroutines {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = ledger.Record(context.Background(), in)
		}(i)
	}
	wg.Wait()

	// All callers must succeed (idempotent — duplicates are silently ignored).
	for i, e := range errs {
		assert.NoError(t, e, "goroutine %d must not return an error", i)
	}

	// Exactly one event must be stored.
	var eventCount int
	require.NoError(t, sqldb.QueryRow(`SELECT COUNT(*) FROM llm_usage_events WHERE user_id='conc-user'`).Scan(&eventCount))
	assert.Equal(t, 1, eventCount, "exactly one event must be inserted for a concurrent same-key batch")

	// Credits must be burned exactly once (only the winning TX calls CommitTransactionalBurn).
	assert.Greater(t, q.credits, int64(0), "credits must be burned by the winning TX")
	// Verify the stub's credit counter matches the single stored event.
	var stored int64
	require.NoError(t, sqldb.QueryRow(`SELECT credits_burned FROM llm_usage_events WHERE user_id='conc-user'`).Scan(&stored))
	assert.Equal(t, stored, q.credits, "stubQuota credits must equal the single event's credits_burned")
}
