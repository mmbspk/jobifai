//go:build !race

package llm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llm"
	"github.com/user/jobifai/internal/llmreuse"
)

func TestClient_DeferredValidation_AbortKeepsGenerationRetryable(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	sqldb, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })
	store := &llmreuse.Store{DB: sqldb}

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		resp := map[string]any{
			"content": []map[string]any{{"type": "text", "text": `{"summary":"tailored ok"}`}},
			"usage":   map[string]any{"input_tokens": 5, "output_tokens": 5},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)

	cfg := domain.LLMConfig{Provider: "claude", Model: "claude-mock", UseProxy: true, ProxyURL: srv.URL, MaxTokens: 1024}
	client := llm.New(cfg, "key").WithUserID("user-1").WithReuse(&llm.ReuseCoordinator{Store: store})
	callCtx := llm.WithTask(ctx, "tailor resume")
	msgs := []llm.Message{{Role: "user", Content: "prompt body"}}

	out1, err := client.Chat(callCtx, msgs)
	require.NoError(t, err)
	require.Contains(t, out1, "tailored ok")
	require.Equal(t, int32(1), calls.Load())
	var state string
	require.NoError(t, sqldb.QueryRow(`SELECT state FROM llm_generation_cache WHERE user_id = ?`, "user-1").Scan(&state))
	require.Equal(t, llmreuse.StateInProgress, state)
	client.AbortReuse(ctx)
	require.NoError(t, sqldb.QueryRow(`SELECT state FROM llm_generation_cache WHERE user_id = ?`, "user-1").Scan(&state))
	require.Equal(t, llmreuse.StateFailedUncertain, state)

	client2 := llm.New(cfg, "key").WithUserID("user-1").WithReuse(&llm.ReuseCoordinator{Store: store})
	out2, err := client2.Chat(callCtx, msgs)
	require.NoError(t, err)
	require.Contains(t, out2, "tailored ok")
	require.Equal(t, int32(2), calls.Load(), "aborted generation must not become a completed cache hit")
	require.NoError(t, client2.CommitValidatedReuse(ctx, out2))

	br, err := store.Begin(ctx, "user-1", domain.TaskResumeTailoring,
		llmreuse.ContentFingerprint("user-1", domain.TaskResumeTailoring, "claude", "claude-mock", "", 1024,
			[]llmreuse.MessagePart{{Role: "user", Content: "prompt body"}}), "", "op-check")
	require.NoError(t, err)
	require.True(t, br.CacheHit)
	require.Contains(t, br.Response, "tailored ok")
}

func TestClient_DeferredValidation_CommitBeforeCacheHit(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	sqldb, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })
	store := &llmreuse.Store{DB: sqldb}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"content": []map[string]any{{"type": "text", "text": "cover letter body"}},
			"usage":   map[string]any{"input_tokens": 5, "output_tokens": 5},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)

	cfg := domain.LLMConfig{Provider: "claude", Model: "claude-mock", UseProxy: true, ProxyURL: srv.URL, MaxTokens: 1024}
	client := llm.New(cfg, "key").WithUserID("user-1").WithReuse(&llm.ReuseCoordinator{Store: store})
	callCtx := llm.WithTask(ctx, "cover letter")
	msgs := []llm.Message{{Role: "user", Content: "write cover"}}

	out, err := client.Chat(callCtx, msgs)
	require.NoError(t, err)
	require.Equal(t, "cover letter body", out)
	require.NoError(t, client.CommitValidatedReuse(ctx, out))

	client2 := llm.New(cfg, "key").WithUserID("user-1").WithReuse(&llm.ReuseCoordinator{Store: store})
	out2, err := client2.Chat(callCtx, msgs)
	require.NoError(t, err)
	require.Equal(t, "cover letter body", out2)
}
