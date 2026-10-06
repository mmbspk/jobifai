package llm_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llm"
	"github.com/user/jobifai/internal/llmreuse"
	"github.com/user/jobifai/internal/pricing"
	"github.com/user/jobifai/internal/quota"
	"github.com/user/jobifai/internal/usage"
)

type reuseQuota struct{ credits atomic.Int64 }

func (q *reuseQuota) RecordLLMBurn(_ context.Context, _ string, credits int64) error {
	q.credits.Add(credits)
	return nil
}
func (q *reuseQuota) PrepareTransactionalBurn(_ string, _ int64) (quota.BurnPrepare, error) {
	return quota.BurnPrepare{}, nil
}
func (q *reuseQuota) CommitTransactionalBurn(_ *sql.Tx, _ string, credits int64, _ quota.BurnPrepare) error {
	q.credits.Add(credits)
	return nil
}

type reuseFixture struct {
	db     *sql.DB
	client *llm.Client
	calls  atomic.Int32
	quota  *reuseQuota
}

func newReuseFixture(t *testing.T) *reuseFixture {
	t.Helper()
	db, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	// Migration 039 adds FK user_id → users on llm_generation_cache.
	// Seed the synthetic user so cache operations don't fail the FK check.
	_, err = db.Exec(`INSERT INTO users (id, email, password_hash, is_admin, created_at)
		VALUES ('u', 'u@reuse.test', 'hash', 0, datetime('now'))`)
	require.NoError(t, err)
	f := &reuseFixture{db: db, quota: &reuseQuota{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		var req struct {
			Messages []llm.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		body := req.Messages[len(req.Messages)-1].Content
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "claude-sonnet-4-6", "content": []map[string]string{{"type": "text", "text": body}}, "usage": map[string]int{"input_tokens": 100, "output_tokens": 20}})
	}))
	t.Cleanup(srv.Close)
	ledger := &usage.Ledger{DB: db, Catalog: pricing.DefaultCatalog(), Quota: f.quota, Defaults: func() domain.QuotaDefaults {
		return domain.QuotaDefaults{CreditsPerUSD: 1000, ServiceMarkup: 0.5, PerCallFeeUSD: 0.002}
	}}
	f.client = llm.New(domain.LLMConfig{Provider: "claude", Model: "claude-sonnet-4-6", UseProxy: true, ProxyURL: srv.URL, MaxTokens: 1024}, "key").WithUserID("u").WithBilling(llm.BillingHooks{Ledger: ledger}).WithReuse(&llm.ReuseCoordinator{Store: &llmreuse.Store{DB: db}}).WithModel("claude-sonnet-4-6", 1024)
	return f
}
func validProse(s string) (string, error) { return s, domain.ValidateCoverContent(s) }
func coverCtx() context.Context           { return llm.WithTask(context.Background(), "cover letter") }
func prompt(s string) []llm.Message       { return []llm.Message{{Role: "user", Content: s}} }
func (f *reuseFixture) events(t *testing.T) int {
	t.Helper()
	var n int
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM llm_usage_events WHERE user_id='u' AND success=1`).Scan(&n))
	return n
}

func TestReuse_ReverseValidationKeepsResponsesWithRequests(t *testing.T) {
	f := newReuseFixture(t)
	aReady, bReady, releaseA := make(chan struct{}), make(chan struct{}), make(chan struct{})
	errs := make(chan error, 2)
	go func() {
		_, err := f.client.ChatValidated(coverCtx(), prompt("Application A cover body"), func(s string) (string, error) { close(aReady); <-releaseA; return validProse(s) })
		errs <- err
	}()
	<-aReady
	go func() {
		_, err := f.client.ChatValidated(coverCtx(), prompt("Application B cover body"), func(s string) (string, error) { close(bReady); return validProse(s) })
		errs <- err
	}()
	<-bReady
	require.NoError(t, <-errs)
	close(releaseA)
	require.NoError(t, <-errs)
	for _, body := range []string{"Application A cover body", "Application B cover body"} {
		out, err := f.client.ChatValidated(coverCtx(), prompt(body), validProse)
		require.NoError(t, err)
		require.Equal(t, body, out)
	}
	require.Equal(t, int32(2), f.calls.Load())
	require.Equal(t, 2, f.events(t))
}

func TestReuse_ConcurrentSamePromptBillsOnce(t *testing.T) {
	f := newReuseFixture(t)
	ready, release := make(chan struct{}), make(chan struct{})
	errs := make(chan error, 2)
	go func() {
		_, err := f.client.ChatValidated(coverCtx(), prompt("One shared cover letter"), func(s string) (string, error) { close(ready); <-release; return validProse(s) })
		errs <- err
	}()
	<-ready
	go func() {
		_, err := f.client.ChatValidated(coverCtx(), prompt("One shared cover letter"), validProse)
		errs <- err
	}()
	close(release)
	require.NoError(t, <-errs)
	require.NoError(t, <-errs)
	require.Equal(t, int32(1), f.calls.Load())
	require.Equal(t, 1, f.events(t))
	require.Positive(t, f.quota.credits.Load())
}

func TestReuse_ValidationFailureRetryAndCrashRecoveryDeduplicateBilling(t *testing.T) {
	f := newReuseFixture(t)
	msgs := prompt("A usable cover letter")
	_, err := f.client.ChatValidated(coverCtx(), msgs, func(string) (string, error) { return "", errors.New("invalid document") })
	require.Error(t, err)
	charged := f.quota.credits.Load()
	require.Positive(t, charged)
	_, err = f.client.ChatValidated(coverCtx(), msgs, validProse)
	require.NoError(t, err)
	require.Equal(t, int32(2), f.calls.Load())
	require.Equal(t, 1, f.events(t))
	require.Equal(t, charged, f.quota.credits.Load())
	// Simulate a restart after billing committed, before cache publication committed.
	_, err = f.db.Exec(`UPDATE llm_generation_cache SET state='in_progress',response_text='',lease_until=datetime('now','-1 minute')`)
	require.NoError(t, err)
	_, err = f.client.ChatValidated(coverCtx(), msgs, validProse)
	require.NoError(t, err)
	require.Equal(t, int32(3), f.calls.Load())
	require.Equal(t, 1, f.events(t))
	require.Equal(t, charged, f.quota.credits.Load())
}

func TestReuse_CanceledValidationReleasesLease(t *testing.T) {
	f := newReuseFixture(t)
	ctx, cancel := context.WithCancel(coverCtx())
	_, err := f.client.ChatValidated(ctx, prompt("A usable cover letter"), func(s string) (string, error) { cancel(); return s, nil })
	require.ErrorIs(t, err, context.Canceled)
	retryCtx, done := context.WithTimeout(coverCtx(), time.Second)
	defer done()
	_, err = f.client.ChatValidated(retryCtx, prompt("A usable cover letter"), validProse)
	require.NoError(t, err)
}
