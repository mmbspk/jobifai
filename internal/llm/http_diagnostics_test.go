package llm_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/llm"
)

func TestTimeoutDiagnosticsReportActualAttemptAndPhase(t *testing.T) {
	var buf bytes.Buffer
	original := log.Logger
	log.Logger = zerolog.New(&buf)
	t.Cleanup(func() { log.Logger = original })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(300 * time.Millisecond):
		}
	}))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := newClaudeClient(t, srv).Chat(ctx, []llm.Message{{Role: "user", Content: "SECRET PROMPT"}})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	output := buf.String()
	require.Contains(t, output, `"event":"llm_http_result"`)
	require.Contains(t, output, `"phase":"waiting_response"`)
	require.Contains(t, output, `"timeout_source":"context_deadline"`)
	require.Contains(t, output, `"attempt":1`)
	require.Contains(t, output, `"attempts":1`)
	require.NotContains(t, output, "SECRET PROMPT")
	require.NotContains(t, output, "fake-key")
	require.NotContains(t, output, srv.URL)
}

func TestHTTPDiagnosticsIncludeStatusAndRequestIDWithoutResponseBody(t *testing.T) {
	var buf bytes.Buffer
	original := log.Logger
	log.Logger = zerolog.New(&buf)
	t.Cleanup(func() { log.Logger = original })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-request-id", "req-123")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("SECRET RESPONSE"))
	}))
	t.Cleanup(srv.Close)
	_, err := newClaudeClient(t, srv).Chat(context.Background(), []llm.Message{{Role: "user", Content: "SECRET PROMPT"}})
	require.Error(t, err)
	output := buf.String()
	require.Contains(t, output, `"http_status":401`)
	require.Contains(t, output, `"provider_request_id":"req-123"`)
	require.Contains(t, output, `"attempts":1`)
	require.NotContains(t, output, "SECRET RESPONSE")
	require.NotContains(t, output, "SECRET PROMPT")
}
