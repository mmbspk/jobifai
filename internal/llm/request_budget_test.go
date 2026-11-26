package llm_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/llm"
	"github.com/user/jobifai/internal/pricing"
	"github.com/user/jobifai/internal/usage"
)

type fixtureRequestBudget struct {
	before    []llm.RequestObservation
	usage     []llm.UsageObservation
	beforeErr error
	usageErr  error
}

func (b *fixtureRequestBudget) BeforeRequest(_ context.Context, o llm.RequestObservation) error {
	b.before = append(b.before, o)
	return b.beforeErr
}
func (b *fixtureRequestBudget) ObserveUsage(_ context.Context, o llm.UsageObservation) error {
	b.usage = append(b.usage, o)
	return b.usageErr
}

func TestRequestBudgetRejectionDoesNotSendOrRetry(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	t.Cleanup(srv.Close)
	blocked := errors.New("fixture request budget exhausted")
	b := &fixtureRequestBudget{beforeErr: blocked}
	_, err := newClaudeClient(t, srv).Chat(llm.WithRequestBudget(context.Background(), b), []llm.Message{{Role: "user", Content: "private prompt"}})
	require.ErrorIs(t, err, blocked)
	require.Zero(t, calls.Load())
	require.Len(t, b.before, 1)
	require.Empty(t, b.usage)
	require.Greater(t, b.before[0].PayloadBytes, len("private prompt"))
	require.Equal(t, 1, b.before[0].Attempt)
}

func TestRequestBudgetObservationRejectionStillPersistsBilling(t *testing.T) {
	srv := claudeOKServer(t, "ok", 5, 2)
	t.Cleanup(srv.Close)
	db, err := appdb.Open(filepath.Join(t.TempDir(), "billing.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	c := newClaudeClient(t, srv).WithUserID("__default__").WithBilling(llm.BillingHooks{Ledger: &usage.Ledger{DB: db, Catalog: pricing.DefaultCatalog()}})
	rejected := errors.New("fixture response exceeds budget")
	b := &fixtureRequestBudget{usageErr: rejected}
	_, err = c.Chat(llm.WithRequestBudget(context.Background(), b), []llm.Message{{Role: "user", Content: "hi"}})
	require.ErrorIs(t, err, rejected)
	require.NotErrorIs(t, err, llm.ErrBillingPersistFailed)
	require.Len(t, b.before, 1)
	require.Len(t, b.usage, 1)
	require.EqualValues(t, 5, b.usage[0].Tokens.InputTokens)
	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM llm_usage_events WHERE success=1").Scan(&count))
	require.Equal(t, 1, count)
}

func TestOutputLimitOnlyLowersAndRetainsOriginalClient(t *testing.T) {
	for _, limit := range []int{1, 10000, 0} {
		t.Run(strconv.Itoa(limit), func(t *testing.T) {
			var captured llm.RequestObservation
			srv := claudeOKServer(t, "ok", 1, 1)
			t.Cleanup(srv.Close)
			c := newClaudeClient(t, srv).WithModel("claude-test", 128)
			b := &fixtureRequestBudget{}
			_, err := c.WithOutputLimit(limit).Chat(llm.WithRequestBudget(context.Background(), b), []llm.Message{{Role: "user", Content: "hi"}})
			require.NoError(t, err)
			captured = b.before[0]
			expected := 128
			if limit > 0 && limit < expected {
				expected = limit
			}
			require.Equal(t, expected, captured.MaxOutputTokens)
			original := &fixtureRequestBudget{}
			_, err = c.Chat(llm.WithRequestBudget(context.Background(), original), []llm.Message{{Role: "user", Content: "hi"}})
			require.NoError(t, err)
			require.Equal(t, 128, original.before[0].MaxOutputTokens)
		})
	}
}

func TestOutputLimitDoesNotRaiseUnconfiguredDefault(t *testing.T) {
	srv := claudeOKServer(t, "ok", 1, 1)
	t.Cleanup(srv.Close)
	b := &fixtureRequestBudget{}
	_, err := newClaudeClient(t, srv).WithOutputLimit(10000).Chat(llm.WithRequestBudget(context.Background(), b), []llm.Message{{Role: "user", Content: "hi"}})
	require.NoError(t, err)
	require.Equal(t, 8192, b.before[0].MaxOutputTokens)
}
