package candidate

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSpec_ID_DistinguishesMaxTokens(t *testing.T) {
	t.Parallel()
	a := Spec{Provider: "claude", Model: "m", Effort: "low", MaxTokens: 1024}
	b := Spec{Provider: "claude", Model: "m", Effort: "low", MaxTokens: 2048}
	require.NotEqual(t, a.ID(), b.ID())
}

func TestSpec_ID_DistinguishesTimeout(t *testing.T) {
	t.Parallel()
	a := Spec{Provider: "claude", Model: "m", MaxTokens: 1024, TimeoutSec: 30}
	b := Spec{Provider: "claude", Model: "m", MaxTokens: 1024, TimeoutSec: 60}
	require.NotEqual(t, a.ID(), b.ID())
}
