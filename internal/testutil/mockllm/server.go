// Package mockllm provides httptest-backed stand-ins for LLM HTTP APIs (no real tokens).
package mockllm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llm"
)

// ClaudeServer returns a test server that responds with a Claude-style JSON envelope.
func ClaudeServer(t *testing.T, text string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"content": []map[string]any{{"type": "text", "text": text}},
			"usage":   map[string]any{"input_tokens": 12, "output_tokens": 8},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

// NewClaudeClient builds an llm.Client pointed at the mock server (UseProxy + ProxyURL).
func NewClaudeClient(t *testing.T, srv *httptest.Server) *llm.Client {
	t.Helper()
	cfg := domain.LLMConfig{
		Provider: "claude",
		Model:    "claude-mock",
		UseProxy: true,
		ProxyURL: srv.URL,
	}
	return llm.New(cfg, "mock-api-key-not-real")
}

// StoreUserLLMConfig saves proxy LLM settings + API key for handler/bot integration tests.
func StoreUserLLMConfig(t *testing.T, cfgStore interface {
	Set(userID, key string, src any) error
}, secrets interface {
	Set(userID, key, value string) error
}, userID, proxyURL string) {
	t.Helper()
	gs := domain.GeneralSettings{}
	gs.LLM = domain.LLMConfig{
		Provider: "claude",
		Model:    "claude-mock",
		UseProxy: true,
		ProxyURL: proxyURL,
	}
	if err := cfgStore.Set(userID, "general_settings", gs); err != nil {
		t.Fatalf("general_settings: %v", err)
	}
	if err := secrets.Set(userID, "llm_api_key", "mock-api-key-not-real"); err != nil {
		t.Fatalf("llm_api_key: %v", err)
	}
}
