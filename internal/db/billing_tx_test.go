package db_test

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appdb "github.com/user/jobifai/internal/db"
)

func TestRecordBillingTx_IdempotencyPreventsDoubleCharge(t *testing.T) {
	t.Parallel()
	sqldb, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })

	in := appdb.LLMUsageEventInput{
		UserID:         "u1",
		Task:           "job_scoring",
		Provider:       "claude",
		RequestedModel: "claude-sonnet-4-6",
		ActualModel:    "claude-sonnet-4-6",
		InputTokens:    100,
		OutputTokens:   50,
		Success:        true,
		LogicalOp:      true,
		IdempotencyKey: "op-1",
		CreditsBurned:  10,
	}
	tx, err := sqldb.Begin()
	require.NoError(t, err)
	_, err = appdb.RecordBillingTx(tx, in, 10, nil)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())

	tx2, err := sqldb.Begin()
	require.NoError(t, err)
	_, err = appdb.RecordBillingTx(tx2, in, 10, nil)
	require.ErrorIs(t, err, appdb.ErrIdempotencyConflict)
	_ = tx2.Rollback()

	var n int
	require.NoError(t, sqldb.QueryRow(`SELECT COUNT(*) FROM llm_usage_events`).Scan(&n))
	assert.Equal(t, 1, n)
}

func TestRecordBillingTx_QuotaFailureRollsBackEvent(t *testing.T) {
	t.Parallel()
	sqldb, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })

	in := appdb.LLMUsageEventInput{
		UserID:         "u1",
		Task:           "job_scoring",
		Provider:       "claude",
		RequestedModel: "m",
		ActualModel:    "m",
		InputTokens:    1,
		OutputTokens:   1,
		Success:        true,
		LogicalOp:      true,
		IdempotencyKey: "op-2",
	}
	tx, err := sqldb.Begin()
	require.NoError(t, err)
	failQuota := func(*sql.Tx, string, int64) error { return errors.New("quota fail") }
	_, err = appdb.RecordBillingTx(tx, in, 5, failQuota)
	require.Error(t, err)
	require.NoError(t, tx.Rollback())

	var n int
	require.NoError(t, sqldb.QueryRow(`SELECT COUNT(*) FROM llm_usage_events`).Scan(&n))
	assert.Equal(t, 0, n)
}
