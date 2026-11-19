package bot

// adaptive_runner_test.go — unit tests for Story #133: adaptive runner wiring.
//
// These tests cover the parts of adaptive_runner.go that don't require a live
// browser — shadow guards, ref parsing, and the Rod-free filler integration via
// stubFiller (defined in step_exec_test.go).

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
)

// ── shadow guards ─────────────────────────────────────────────────────────────

func TestShadowEasyApply_ReturnsGuardError(t *testing.T) {
	err := shadowEasyApply()
	require.Error(t, err)
	assert.True(t, errors.Is(err, errShadowBrowserAction))
}

func TestShadowSeekApply_ReturnsGuardError(t *testing.T) {
	err := shadowSeekApply()
	require.Error(t, err)
	assert.True(t, errors.Is(err, errShadowBrowserAction))
}

// ── adaptiveEasyApply shadow guard ────────────────────────────────────────────

func TestAdaptiveEasyApply_ShadowEngine_ReturnsGuardError(t *testing.T) {
	b := newTestBot(t)
	ac := &AttemptContext{Engine: domain.ApplyEngineShadow}

	// lazy=nil is irrelevant — shadow guard fires before any lazy check.
	err := b.adaptiveEasyApply(context.Background(), nil, nil, ac)
	require.Error(t, err)
	assert.True(t, errors.Is(err, errShadowBrowserAction), "expected shadow guard error, got: %v", err)
}

func TestAdaptiveEasyApply_NilLazy_ReturnsIdentityError(t *testing.T) {
	b := newTestBot(t)
	ac := &AttemptContext{Engine: domain.ApplyEngineAdaptive}

	err := b.adaptiveEasyApply(context.Background(), nil, nil, ac)
	require.Error(t, err)
	assert.True(t, errors.Is(err, errLinkedInIdentity), "expected identity error for nil lazy, got: %v", err)
}

// ── refIndex ──────────────────────────────────────────────────────────────────

func TestRefIndex_ValidRefs(t *testing.T) {
	cases := []struct {
		ref  string
		want int
	}{
		{"f0", 0},
		{"f1", 1},
		{"f9", 9},
		{"f10", 10},
		{"f99", 99},
	}
	for _, tc := range cases {
		got, err := refIndex(tc.ref)
		require.NoError(t, err, "ref=%s", tc.ref)
		assert.Equal(t, tc.want, got, "ref=%s", tc.ref)
	}
}

func TestRefIndex_InvalidRefs(t *testing.T) {
	cases := []string{"", "0", "x0", "field0", "f", "f-1", "fabc"}
	for _, ref := range cases {
		_, err := refIndex(ref)
		require.Error(t, err, "ref=%q should error", ref)
	}
}

// ── routing invariant ─────────────────────────────────────────────────────────

// TestAdaptiveRouting_LegacyEngineDoesNotCallAdaptive verifies that when the
// bot manager is configured for legacy, the legacy applyErr path is used.
// This is a documentation test — the actual routing lives in manager.go but
// is validated here by ensuring the adaptive entry points have clear guards.
func TestAdaptiveRouting_ShadowGuardNilAC(t *testing.T) {
	b := newTestBot(t)
	// nil AttemptContext must not panic — shadow check is nil-safe.
	err := b.adaptiveEasyApply(context.Background(), nil, nil, nil)
	// Should fail with identity error, not a nil pointer panic.
	require.Error(t, err)
	assert.False(t, errors.Is(err, errShadowBrowserAction))
	assert.True(t, errors.Is(err, errLinkedInIdentity))
}

// ── newTestBot helper ─────────────────────────────────────────────────────────

// newTestBot creates a minimal Bot for testing adaptive_runner functions that
// don't need a full DB or browser. Uses an in-memory config with no-op fields.
func newTestBot(t *testing.T) *Bot {
	t.Helper()
	return &Bot{
		cfg: Config{
			UserID: "test-user",
		},
	}
}
