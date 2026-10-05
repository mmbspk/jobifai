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

	_, err = sqldb.Exec(`
		INSERT INTO users (id, email, password_hash, is_admin, created_at)
		VALUES ('u1', 'u1@billing.test', 'hash', 0, datetime('now'))`)
	require.NoError(t, err)
	_, err = sqldb.Exec(`
		INSERT INTO user_quota (user_id, plan, enforcement_enabled, trial_remaining_micro, updated_at)
		VALUES ('u1', 'trial', 1, 1000, CURRENT_TIMESTAMP)`)
	require.NoError(t, err)
	_, err = sqldb.Exec(`
		INSERT INTO usage_totals (user_id, model, input_tokens, output_tokens, calls, updated_at)
		VALUES ('u1', 'm', 10, 5, 1, CURRENT_TIMESTAMP)`)
	require.NoError(t, err)

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

	var events, inTok, calls int64
	var trial int64
	require.NoError(t, sqldb.QueryRow(`SELECT COUNT(*) FROM llm_usage_events`).Scan(&events))
	require.NoError(t, sqldb.QueryRow(`SELECT input_tokens, calls FROM usage_totals WHERE user_id='u1' AND model='m'`).Scan(&inTok, &calls))
	require.NoError(t, sqldb.QueryRow(`SELECT trial_remaining_micro FROM user_quota WHERE user_id='u1'`).Scan(&trial))
	assert.Equal(t, int64(0), events)
	assert.Equal(t, int64(10), inTok)
	assert.Equal(t, int64(1), calls)
	assert.Equal(t, int64(1000), trial)
}
