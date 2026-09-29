package llm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llm"
	"github.com/user/jobifai/internal/pricing"
	"github.com/user/jobifai/internal/quota"
	"github.com/user/jobifai/internal/usage"
)

func TestWithUserID_AttributionWithoutQuota(t *testing.T) {
	t.Parallel()
	srv := claudeOKServer(t, "ok", 3, 2)
	t.Cleanup(srv.Close)

	sqldb, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })

	ledger := &usage.Ledger{DB: sqldb, Catalog: pricing.DefaultCatalog()}
	client := newClaudeClient(t, srv).
		WithUserID("__default__").
		WithBilling(llm.BillingHooks{Ledger: ledger})

	ctx := llm.WithCallContext(context.Background(), domain.LLMCallContext{Task: domain.TaskJobScoring})
	_, err = client.Chat(ctx, []llm.Message{{Role: "user", Content: "hi"}})
	require.NoError(t, err)

	var uid string
	require.NoError(t, sqldb.QueryRow(`SELECT user_id FROM llm_usage_events LIMIT 1`).Scan(&uid))
	assert.Equal(t, "__default__", uid)
}

func TestChat_BillingPersistFailureSurfaces(t *testing.T) {
	t.Parallel()
	srv := claudeOKServer(t, "ok", 1, 1)
	t.Cleanup(srv.Close)

	// Ledger with nil DB forces persistence error when billing is wired.
	ledger := &usage.Ledger{Catalog: pricing.DefaultCatalog()}
	client := newClaudeClient(t, srv).
		WithUserID("user-1").
		WithBilling(llm.BillingHooks{Ledger: ledger})

	_, err := client.Chat(context.Background(), []llm.Message{{Role: "user", Content: "hi"}})
	require.Error(t, err)
	assert.ErrorIs(t, err, llm.ErrBillingPersistFailed)
}

func TestChat_BillingPersistFailure_NoProviderRetry(t *testing.T) {
	t.Parallel()
	var providerCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{{"type": "text", "text": "ok"}},
			"usage":   map[string]any{"input_tokens": 5, "output_tokens": 2},
		})
	}))
	t.Cleanup(srv.Close)

	ledger := &usage.Ledger{Catalog: pricing.DefaultCatalog()}
	cfg := domain.LLMConfig{Provider: "claude", Model: "claude-test", UseProxy: true, ProxyURL: srv.URL}
	client := llm.New(cfg, "key").
		WithUserID("user-1").
		WithBilling(llm.BillingHooks{Ledger: ledger})

	_, err := client.Chat(context.Background(), []llm.Message{{Role: "user", Content: "hi"}})
	require.Error(t, err)
	assert.ErrorIs(t, err, llm.ErrBillingPersistFailed)
	assert.EqualValues(t, 1, providerCalls.Load(), "billing persistence failure must not retry provider HTTP")
}

func TestCheckQuota_UsesUserIDWithoutWithQuotaUserArg(t *testing.T) {
	t.Parallel()
	guard := &recordingGuard{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{{"type": "text", "text": "x"}},
			"usage":   map[string]any{"input_tokens": 1, "output_tokens": 1},
		})
	}))
	t.Cleanup(srv.Close)
	cfg := domain.LLMConfig{Provider: "claude", Model: "m", UseProxy: true, ProxyURL: srv.URL}
	c := llm.New(cfg, "k").WithUserID("user-abc").WithQuota(guard, "ignored")
	_, err := c.Chat(context.Background(), []llm.Message{{Role: "user", Content: "hi"}})
	require.NoError(t, err)
	require.Equal(t, "user-abc", guard.lastUser)
}

type recordingGuard struct {
	lastUser string
}

func (r *recordingGuard) BeforeLLM(_ context.Context, userID, _ string, _, _ int) error {
	r.lastUser = userID
	return nil
}

func (r *recordingGuard) RecordLLM(context.Context, string, string, int64, int64) error {
	return nil
}

var _ quota.LLMGuard = (*recordingGuard)(nil)
