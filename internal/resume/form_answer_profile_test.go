package resume_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llm"
	"github.com/user/jobifai/internal/resume"
)

func TestFormAnswer_ProviderReceivesReadableCurrentProfile(t *testing.T) {
	var calls atomic.Int32
	prompts := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var request struct {
			Messages []llm.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		prompts <- request.Messages[0].Content
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"content": []map[string]string{{"type": "text", "text": "1 month"}}, "usage": map[string]int{"input_tokens": 50, "output_tokens": 10}})
	}))
	defer server.Close()
	client := llm.New(domain.LLMConfig{Provider: "claude", Model: "mock", UseProxy: true, ProxyURL: server.URL}, "fixture")
	tailor := resume.NewTailor(client, client, client, client)
	profile := []byte(`{"personal_information":{"name":"Amina Sample"},"application_defaults":{"notice_period":"1 month"}}`)
	answer, err := tailor.AnswerFormQuestion(context.Background(), profile, "How soon can you leave?", nil)
	require.NoError(t, err)
	assert.Equal(t, "1 month", answer)
	prompt := <-prompts
	assert.Contains(t, prompt, `"name":"Amina Sample"`)
	assert.Contains(t, prompt, `"notice_period":"1 month"`)
	_, err = tailor.AnswerFormQuestion(context.Background(), []byte("invalid JSON"), "How soon can you leave?", nil)
	require.Error(t, err)
	assert.Equal(t, int32(1), calls.Load(), "invalid profile data must not reach the provider")
}
