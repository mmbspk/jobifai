package bot_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/bot"
	"github.com/user/jobifai/internal/browser"
	"github.com/user/jobifai/internal/config"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
)

func newTestManager(t *testing.T) *bot.Manager {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	cfgStore := config.NewStore(db)
	secrets := config.NewSecretsStore(db, "test-key")
	sessions := browser.NewSessionStore(db, secrets)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	return bot.NewManager(ctx, db, cfgStore, secrets, sessions, nil, nil, nil, nil, "")
}

func TestManager_Status_NewUser_ReturnsIdle(t *testing.T) {
	mgr := newTestManager(t)
	status := mgr.Status("user-1")
	assert.Equal(t, domain.BotStateIdle, status.State)
}

func TestManager_Stop_NoBot_IsNoop(t *testing.T) {
	mgr := newTestManager(t)
	// Should not panic or error.
	mgr.Stop("user-1")
	status := mgr.Status("user-1")
	assert.Equal(t, domain.BotStateIdle, status.State)
}

func TestManager_Pause_NoBot_IsNoop(t *testing.T) {
	mgr := newTestManager(t)
	mgr.Pause("user-1")
	// State should remain idle (Pause only affects a running bot).
	status := mgr.Status("user-1")
	assert.Equal(t, domain.BotStateIdle, status.State)
}

func TestManager_Resume_NoBot_IsNoop(t *testing.T) {
	mgr := newTestManager(t)
	mgr.Resume("user-1")
	status := mgr.Status("user-1")
	assert.Equal(t, domain.BotStateIdle, status.State)
}

func TestManager_Start_NoProfile_ReturnsError(t *testing.T) {
	mgr := newTestManager(t)
	err := mgr.Start(context.Background(), "user-1", domain.PlatformLinkedIn)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resume profile")
}

func TestManager_Start_UnsupportedPlatform_ReturnsError(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	cfgStore := config.NewStore(db)
	secrets := config.NewSecretsStore(db, "test-key")
	sessions := browser.NewSessionStore(db, secrets)

	// Seed a resume profile so buildConfig passes the profile check.
	require.NoError(t, cfgStore.Set("user-1", "resume_profile", map[string]any{"summary": "test"}))

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	mgr := bot.NewManager(ctx, db, cfgStore, secrets, sessions, nil, nil, nil, nil, "")
	err = mgr.Start(context.Background(), "user-1", domain.Platform("twitter"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported platform")
}
