package llm_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llm"
)

// claudeOKServer returns a valid Claude API response with the given text.
func claudeOKServer(t *testing.T, text string, inTokens, outTokens int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"content": []map[string]any{{"type": "text", "text": text}},
			"usage":   map[string]any{"input_tokens": inTokens, "output_tokens": outTokens},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func newClaudeClient(t *testing.T, srv *httptest.Server) *llm.Client {
	t.Helper()
	cfg := domain.LLMConfig{
		Provider: "claude",
		Model:    "claude-test",
		UseProxy: true,
		ProxyURL: srv.URL,
	}
	return llm.New(cfg, "fake-key")
}

// ── Chat: happy path ──────────────────────────────────────────────────────────

func TestChat_ClaudeSuccess(t *testing.T) {
	srv := claudeOKServer(t, "hello world", 10, 5)
	t.Cleanup(srv.Close)

	client := newClaudeClient(t, srv)
	got, err := client.Chat(context.Background(), []llm.Message{{Role: "user", Content: "hi"}})
	require.NoError(t, err)
	assert.Equal(t, "hello world", got)
}

// ── Chat: non-retryable 4xx stops immediately ─────────────────────────────────

func TestChat_NonRetryable401(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, `{"error":{"message":"unauthorized"}}`, http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	client := newClaudeClient(t, srv)
	_, err := client.Chat(context.Background(), []llm.Message{{Role: "user", Content: "hi"}})
	require.Error(t, err)
	assert.EqualValues(t, 1, calls.Load(), "401 must not be retried")
}

// ── Chat: 429 is retried ──────────────────────────────────────────────────────

func TestChat_429IsRetried(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n < 3 {
			http.Error(w, `{"error":{"message":"rate limit"}}`, http.StatusTooManyRequests)
			return
		}
		resp := map[string]any{
			"content": []map[string]any{{"type": "text", "text": "ok"}},
			"usage":   map[string]any{"input_tokens": 1, "output_tokens": 1},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)

	client := newClaudeClient(t, srv)
	got, err := client.Chat(context.Background(), []llm.Message{{Role: "user", Content: "hi"}})
	require.NoError(t, err)
	assert.Equal(t, "ok", got)
	assert.EqualValues(t, 3, calls.Load(), "must have retried twice before success")
}

// ── Chat: 5xx is retried then gives up ───────────────────────────────────────

func TestChat_5xxRetriesExhausted(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	client := newClaudeClient(t, srv)
	_, err := client.Chat(context.Background(), []llm.Message{{Role: "user", Content: "hi"}})
	require.Error(t, err)
	assert.EqualValues(t, 3, calls.Load(), "should attempt 3 times total")
}

// ── WithModel ─────────────────────────────────────────────────────────────────

func TestWithModel_OverridesModelAndTokens(t *testing.T) {
	var receivedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&receivedBody)
		resp := map[string]any{
			"content": []map[string]any{{"type": "text", "text": "ok"}},
			"usage":   map[string]any{"input_tokens": 1, "output_tokens": 1},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)

	base := newClaudeClient(t, srv)
	derived := base.WithModel("claude-custom-model", 1024)
	_, err := derived.Chat(context.Background(), []llm.Message{{Role: "user", Content: "hi"}})
	require.NoError(t, err)

	assert.Equal(t, "claude-custom-model", receivedBody["model"])
	assert.EqualValues(t, 1024, receivedBody["max_tokens"])
}

func TestWithModel_ZeroMaxTokensKeepsCurrent(t *testing.T) {
	var receivedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&receivedBody)
		resp := map[string]any{
			"content": []map[string]any{{"type": "text", "text": "ok"}},
			"usage":   map[string]any{"input_tokens": 1, "output_tokens": 1},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)

	cfg := domain.LLMConfig{Provider: "claude", Model: "base-model", MaxTokens: 512, UseProxy: true, ProxyURL: srv.URL}
	base := llm.New(cfg, "fake-key")
	derived := base.WithModel("other-model", 0)
	_, err := derived.Chat(context.Background(), []llm.Message{{Role: "user", Content: "hi"}})
	require.NoError(t, err)
	assert.EqualValues(t, 512, receivedBody["max_tokens"])
}

// ── WithTracker ───────────────────────────────────────────────────────────────

func TestWithTracker_AccumulatesTokens(t *testing.T) {
	srv := claudeOKServer(t, "response", 20, 10)
	t.Cleanup(srv.Close)

	tracker := &llm.UsageTracker{}
	client := newClaudeClient(t, srv).WithTracker(tracker)

	_, err := client.Chat(context.Background(), []llm.Message{{Role: "user", Content: "a"}})
	require.NoError(t, err)
	_, err = client.Chat(context.Background(), []llm.Message{{Role: "user", Content: "b"}})
	require.NoError(t, err)

	snap := tracker.Snapshot()
	assert.EqualValues(t, 40, snap.InputTokens)
	assert.EqualValues(t, 20, snap.OutputTokens)
	assert.Equal(t, 2, snap.Calls)
}

func TestWithTracker_NoTrackerNoPanic(t *testing.T) {
	srv := claudeOKServer(t, "ok", 1, 1)
	t.Cleanup(srv.Close)

	// No tracker attached — should not panic
	client := newClaudeClient(t, srv)
	_, err := client.Chat(context.Background(), []llm.Message{{Role: "user", Content: "hi"}})
	require.NoError(t, err)
}

// ── WithModel does not share tracker with base ────────────────────────────────

func TestWithModel_InheritsTracker(t *testing.T) {
	srv := claudeOKServer(t, "ok", 5, 3)
	t.Cleanup(srv.Close)

	tracker := &llm.UsageTracker{}
	base := newClaudeClient(t, srv).WithTracker(tracker)
	derived := base.WithModel("override-model", 0)

	_, err := derived.Chat(context.Background(), []llm.Message{{Role: "user", Content: "hi"}})
	require.NoError(t, err)

	snap := tracker.Snapshot()
	assert.EqualValues(t, 5, snap.InputTokens, "derived client should share tracker with base")
	assert.Equal(t, 1, snap.Calls)
}

// ── Unknown provider ──────────────────────────────────────────────────────────

func TestChat_UnknownProvider(t *testing.T) {
	cfg := domain.LLMConfig{Provider: "unknown-llm", Model: "test"}
	client := llm.New(cfg, "key")
	_, err := client.Chat(context.Background(), []llm.Message{{Role: "user", Content: "hi"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown LLM provider")
}

// ── OpenAI provider ───────────────────────────────────────────────────────────

func TestChat_OpenAISuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": "openai reply"}}},
			"usage":   map[string]any{"prompt_tokens": 3, "completion_tokens": 2},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)

	cfg := domain.LLMConfig{Provider: "openai", Model: "gpt-test", UseProxy: true, ProxyURL: srv.URL}
	client := llm.New(cfg, "fake-key")
	got, err := client.Chat(context.Background(), []llm.Message{{Role: "user", Content: "hi"}})
	require.NoError(t, err)
	assert.Equal(t, "openai reply", got)
}

// ── Context cancellation propagates ──────────────────────────────────────────

func TestChat_ContextCancelledBeforeCall(t *testing.T) {
	srv := claudeOKServer(t, "ok", 1, 1)
	t.Cleanup(srv.Close)

	client := newClaudeClient(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := client.Chat(ctx, []llm.Message{{Role: "user", Content: "hi"}})
	require.Error(t, err)
}

// ── llm_request_result lifecycle event ───────────────────────────────────────

// captureGlobalLog replaces the global zerolog logger with a JSON-writing buffer
// for the duration of the test. The buffer collects every log line emitted via
// log.Info() / log.Error() etc. (which is what logLLMResult uses).
func captureGlobalLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	orig := log.Logger
	log.Logger = zerolog.New(buf)
	t.Cleanup(func() { log.Logger = orig })
	return buf
}

// parseEventLine scans buf for a JSON log line carrying the given event field value.
// Returns the parsed map or nil if not found.
func parseEventLine(t *testing.T, buf *bytes.Buffer, event string) map[string]any {
	t.Helper()
	for _, line := range bytes.Split(buf.Bytes(), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(line, &m); err != nil {
			continue
		}
		if m["event"] == event {
			return m
		}
	}
	return nil
}

// TestLLMResultEvent_SuccessFields verifies that a successful Claude call emits an
// llm_request_result event with schema_version, automation_run_id, and user_id.
func TestLLMResultEvent_SuccessFields(t *testing.T) {
	srv := claudeOKServer(t, "hello", 10, 5)
	t.Cleanup(srv.Close)
	buf := captureGlobalLog(t)

	client := newClaudeClient(t, srv)
	cc := domain.LLMCallContext{
		UserID:          "test-user",
		AutomationRunID: "run-abc",
		JobID:           "job-99",
		Task:            "evaluate job",
	}
	ctx := llm.WithCallContext(context.Background(), cc)

	_, err := client.Chat(ctx, []llm.Message{{Role: "user", Content: "evaluate this job"}})
	require.NoError(t, err)

	m := parseEventLine(t, buf, "llm_request_result")
	require.NotNil(t, m, "llm_request_result event must be emitted on success")

	assert.Equal(t, "1", m["schema_version"], "schema_version must be '1'")
	assert.Equal(t, true, m["success"], "success must be true")
	assert.Equal(t, "test-user", m["user_id"], "user_id must match call context")
	assert.Equal(t, "run-abc", m["automation_run_id"], "automation_run_id must match (not run_id)")
	assert.Equal(t, "job-99", m["job_id"], "job_id must match call context")
	assert.Equal(t, "claude", m["provider"], "provider")
	assert.Equal(t, "claude-test", m["requested_model"], "requested_model")
	assert.NotEmpty(t, m["actual_model"], "actual_model must be present")
	assert.InDelta(t, float64(10), m["input_tokens"], 0, "input_tokens")
	assert.InDelta(t, float64(5), m["output_tokens"], 0, "output_tokens")
	assert.Nil(t, m["failure_code"], "failure_code must not appear on success")
}

// TestLLMResultEvent_FailureEmitted verifies that a failed Claude call also emits
// an llm_request_result event, with success=false and a failure_code.
func TestLLMResultEvent_FailureEmitted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return a permanent 401 (non-retryable).
		http.Error(w, `{"error":{"message":"unauthorized"}}`, http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	buf := captureGlobalLog(t)

	client := newClaudeClient(t, srv)
	cc := domain.LLMCallContext{
		UserID:          "fail-user",
		AutomationRunID: "run-fail",
	}
	ctx := llm.WithCallContext(context.Background(), cc)

	_, err := client.Chat(ctx, []llm.Message{{Role: "user", Content: "hi"}})
	require.Error(t, err)

	m := parseEventLine(t, buf, "llm_request_result")
	require.NotNil(t, m, "llm_request_result must be emitted even on failure")

	assert.Equal(t, false, m["success"], "success must be false on error")
	assert.Equal(t, "fail-user", m["user_id"])
	assert.Equal(t, "run-fail", m["automation_run_id"])
	assert.NotEmpty(t, m["failure_code"], "failure_code must be set on failure")
	assert.Equal(t, "auth_failed", m["failure_code"], "401 must map to auth_failed")
	assert.Nil(t, m["input_tokens"], "input_tokens must not appear on failure")
}
