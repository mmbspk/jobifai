package mockllm_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/llm"
	"github.com/user/jobifai/internal/testutil/mockllm"
)

func TestClaudeServer_ReturnsTextToClient(t *testing.T) {
	srv := mockllm.ClaudeServer(t, "mock-response")
	t.Cleanup(srv.Close)

	client := mockllm.NewClaudeClient(t, srv)
	out, err := client.Chat(context.Background(), []llm.Message{{Role: "user", Content: "ping"}})
	require.NoError(t, err)
	assert.Equal(t, "mock-response", out)
}
