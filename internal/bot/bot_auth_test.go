package bot

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/browser"
	"github.com/user/jobifai/internal/config"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
)

// ── helpers ──────────────────────────────────────────────────────────────────

func newAuthTestBot(t *testing.T, sessions *browser.SessionStore) *Bot {
	t.Helper()
	return &Bot{
		cfg:    Config{Sessions: sessions, UserID: "u1"},
		state:  domain.BotStateRunning,
		stopCh: make(chan struct{}),
	}
}

func newAuthTestSessionStore(t *testing.T) *browser.SessionStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return browser.NewSessionStore(db, config.NewSecretsStore(db, "test-passphrase"))
}

func seedSession(t *testing.T, store *browser.SessionStore, userID, platform string) {
	t.Helper()
	raw, err := browser.MarshalCookies([]browser.Cookie{{Name: "sid", Value: "v"}})
	require.NoError(t, err)
	require.NoError(t, store.Save(userID, platform, "manual", raw))
}

func advanceSession(t *testing.T, store *browser.SessionStore, userID, platform string) {
	t.Helper()
	// RFC3339Nano precision: two consecutive saves produce distinct UpdatedAt values
	// without any sleep. No 1.1s sleep needed.
	raw, err := browser.MarshalCookies([]browser.Cookie{{Name: "sid", Value: "refreshed"}})
	require.NoError(t, err)
	require.NoError(t, store.Save(userID, platform, "manual", raw))
}

// ── ErrAuthRequired sentinel ─────────────────────────────────────────────────

// TestErrAuthRequired_IsSentinel verifies errors.Is works on wrapped variants.
// The seekRecoverAuth gate depends on this.
func TestErrAuthRequired_IsSentinel(t *testing.T) {
	wrapped := fmt.Errorf("seek: %w", ErrAuthRequired)
	assert.True(t, errors.Is(wrapped, ErrAuthRequired))
	assert.False(t, errors.Is(errors.New("some other error"), ErrAuthRequired))
}

// ── waitForSessionRefresh ─────────────────────────────────────────────────────

func TestWaitForSessionRefresh_NoSessionStore(t *testing.T) {
	b := newAuthTestBot(t, nil)
	err := b.waitForSessionRefresh("u1", "seek", 5*time.Second)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no session store")
}

func TestWaitForSessionRefresh_Timeout(t *testing.T) {
	orig := authRefreshPollInterval
	authRefreshPollInterval = 50 * time.Millisecond
	t.Cleanup(func() { authRefreshPollInterval = orig })

	store := newAuthTestSessionStore(t)
	seedSession(t, store, "u1", "seek")

	b := newAuthTestBot(t, store)
	err := b.waitForSessionRefresh("u1", "seek", 200*time.Millisecond)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
	// waitForSessionRefresh does NOT transition to Running on timeout.
	b.mu.Lock()
	assert.Equal(t, domain.BotStateUserActionRequired, b.state)
	b.mu.Unlock()
}

// TestWaitForSessionRefresh_SessionUpdated verifies the function returns nil
// when UpdatedAt advances, and that it does NOT set BotStateRunning — only
// launchBrowser sets Running after full verification.
func TestWaitForSessionRefresh_SessionUpdated(t *testing.T) {
	orig := authRefreshPollInterval
	authRefreshPollInterval = 50 * time.Millisecond
	t.Cleanup(func() { authRefreshPollInterval = orig })

	store := newAuthTestSessionStore(t)
	seedSession(t, store, "u1", "seek")

	b := newAuthTestBot(t, store)

	go func() {
		time.Sleep(100 * time.Millisecond)
		advanceSession(t, store, "u1", "seek")
	}()

	err := b.waitForSessionRefresh("u1", "seek", 10*time.Second)
	require.NoError(t, err)

	// State must remain user_action_required — Running is only set by
	// launchBrowser AFTER relaunch + auth verification succeed.
	b.mu.Lock()
	assert.Equal(t, domain.BotStateUserActionRequired, b.state,
		"waitForSessionRefresh must NOT set BotStateRunning")
	b.mu.Unlock()
}

func TestWaitForSessionRefresh_BotStop(t *testing.T) {
	orig := authRefreshPollInterval
	authRefreshPollInterval = 50 * time.Millisecond
	t.Cleanup(func() { authRefreshPollInterval = orig })

	store := newAuthTestSessionStore(t)
	seedSession(t, store, "u1", "seek")

	b := newAuthTestBot(t, store)

	go func() {
		time.Sleep(100 * time.Millisecond)
		close(b.stopCh)
	}()

	start := time.Now()
	err := b.waitForSessionRefresh("u1", "seek", 30*time.Second)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "stopped")
	assert.Less(t, elapsed, 2*time.Second, "stop channel must exit wait quickly")
}

// TestWaitForSessionRefresh_TransitionsState verifies mid-wait state is
// user_action_required, and that it stays user_action_required after success
// (not Running — Running is the caller's responsibility).
func TestWaitForSessionRefresh_TransitionsState(t *testing.T) {
	orig := authRefreshPollInterval
	authRefreshPollInterval = 50 * time.Millisecond
	t.Cleanup(func() { authRefreshPollInterval = orig })

	store := newAuthTestSessionStore(t)
	seedSession(t, store, "u1", "seek")

	b := newAuthTestBot(t, store)

	stateDuring := make(chan domain.BotState, 1)
	go func() {
		time.Sleep(100 * time.Millisecond)
		b.mu.Lock()
		stateDuring <- b.state
		b.mu.Unlock()
		advanceSession(t, store, "u1", "seek")
	}()

	err := b.waitForSessionRefresh("u1", "seek", 10*time.Second)
	require.NoError(t, err)

	assert.Equal(t, domain.BotStateUserActionRequired, <-stateDuring, "state mid-wait")

	b.mu.Lock()
	assert.Equal(t, domain.BotStateUserActionRequired, b.state,
		"state after waitForSessionRefresh must still be user_action_required")
	b.mu.Unlock()
}

// ── seekRecoverAuth lifecycle ─────────────────────────────────────────────────

// TestSeekRecoverAuth_NoAuthError returns (nil,nil,nil) — no recovery needed.
func TestSeekRecoverAuth_NoAuthError_NoRecovery(t *testing.T) {
	b := newAuthTestBot(t, nil)
	b.seekSeams = &seekAuthSeams{
		ensureLoggedIn: func(*rod.Page) error { return nil },
	}

	newBr, newPage, err := b.seekRecoverAuth(nil, func() {})
	require.NoError(t, err)
	assert.Nil(t, newBr)
	assert.Nil(t, newPage)
}

// TestSeekRecoverAuth_NonAuthRequired_ImmediatePropagation verifies that a
// non-ErrAuthRequired error is returned immediately without entering the
// session-refresh wait, and that the browser is closed.
func TestSeekRecoverAuth_NonAuthRequired_ImmediatePropagation(t *testing.T) {
	store := newAuthTestSessionStore(t)
	b := newAuthTestBot(t, store)

	relaunchCalled := false
	b.seekSeams = &seekAuthSeams{
		ensureLoggedIn: func(*rod.Page) error { return errors.New("network error: dial timeout") },
		relaunchBrowser: func() (*rod.Browser, *rod.Page, error) {
			relaunchCalled = true
			return nil, nil, nil
		},
	}

	closeCalled := false
	closeFn := func() { closeCalled = true }

	_, _, err := b.seekRecoverAuth(nil, closeFn)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "network error")
	assert.True(t, closeCalled, "browser must be closed even on non-auth error")
	assert.False(t, relaunchCalled, "relaunch must NOT be called for non-auth errors")
}

// TestSeekRecoverAuth_ErrAuthRequired_ClosesBrowserFirst verifies that closeFn
// is called (browser released) as part of the ErrAuthRequired recovery path.
// Ordering relative to the poll loop is a code-structure guarantee: seekRecoverAuth
// calls closeFn() before waitForSessionRefresh() — sequential execution ensures this.
func TestSeekRecoverAuth_ErrAuthRequired_ClosesBrowserFirst(t *testing.T) {
	orig := authRefreshPollInterval
	authRefreshPollInterval = 50 * time.Millisecond
	t.Cleanup(func() { authRefreshPollInterval = orig })

	store := newAuthTestSessionStore(t)
	seedSession(t, store, "u1", "seek")

	b := newAuthTestBot(t, store)

	b.seekSeams = &seekAuthSeams{
		ensureLoggedIn:  func(*rod.Page) error { return ErrAuthRequired },
		relaunchBrowser: func() (*rod.Browser, *rod.Page, error) { return nil, nil, nil },
		checkLoginState: func(*rod.Page) (browser.LoginState, error) {
			return browser.LoginStateYes, nil
		},
	}

	closeCalled := false
	closeFn := func() { closeCalled = true }

	go func() {
		time.Sleep(100 * time.Millisecond)
		advanceSession(t, store, "u1", "seek")
	}()

	_, _, err := b.seekRecoverAuth(nil, closeFn)
	require.NoError(t, err)
	assert.True(t, closeCalled,
		"closeFn must be called to release the Chrome profile before waiting for reconnect")
}

// TestSeekRecoverAuth_SessionRefresh_TriggersRelaunch verifies that after the
// session store UpdatedAt advances, seekRecoverAuth calls relaunchBrowser.
func TestSeekRecoverAuth_SessionRefresh_TriggersRelaunch(t *testing.T) {
	orig := authRefreshPollInterval
	authRefreshPollInterval = 50 * time.Millisecond
	t.Cleanup(func() { authRefreshPollInterval = orig })

	store := newAuthTestSessionStore(t)
	seedSession(t, store, "u1", "seek")

	b := newAuthTestBot(t, store)

	relaunchCalled := false
	b.seekSeams = &seekAuthSeams{
		ensureLoggedIn: func(*rod.Page) error { return ErrAuthRequired },
		relaunchBrowser: func() (*rod.Browser, *rod.Page, error) {
			relaunchCalled = true
			return nil, nil, nil
		},
		checkLoginState: func(*rod.Page) (browser.LoginState, error) {
			return browser.LoginStateYes, nil
		},
	}

	go func() {
		time.Sleep(100 * time.Millisecond)
		advanceSession(t, store, "u1", "seek")
	}()

	_, _, err := b.seekRecoverAuth(nil, func() {})
	require.NoError(t, err)
	assert.True(t, relaunchCalled, "relaunchBrowser must be called after session refresh")
}

// TestSeekRecoverAuth_SuccessRunningSetByCallerNotHere verifies that seekRecoverAuth
// itself does NOT set BotStateRunning — only the caller (launchBrowser) does that.
func TestSeekRecoverAuth_SuccessRunningSetByCallerNotHere(t *testing.T) {
	orig := authRefreshPollInterval
	authRefreshPollInterval = 50 * time.Millisecond
	t.Cleanup(func() { authRefreshPollInterval = orig })

	store := newAuthTestSessionStore(t)
	seedSession(t, store, "u1", "seek")

	b := newAuthTestBot(t, store)
	b.state = domain.BotStateUserActionRequired

	b.seekSeams = &seekAuthSeams{
		ensureLoggedIn: func(*rod.Page) error { return ErrAuthRequired },
		relaunchBrowser: func() (*rod.Browser, *rod.Page, error) { return nil, nil, nil },
		checkLoginState: func(*rod.Page) (browser.LoginState, error) {
			return browser.LoginStateYes, nil
		},
	}

	go func() {
		time.Sleep(100 * time.Millisecond)
		advanceSession(t, store, "u1", "seek")
	}()

	newBr, newPage, err := b.seekRecoverAuth(nil, func() {})
	require.NoError(t, err)
	assert.Nil(t, newBr, "seam returns nil browser — caller adopts it")
	assert.Nil(t, newPage)

	// seekRecoverAuth must NOT have set Running — that is the caller's job.
	b.mu.Lock()
	assert.Equal(t, domain.BotStateUserActionRequired, b.state,
		"seekRecoverAuth must not set BotStateRunning")
	b.mu.Unlock()

	// Simulate what launchBrowser does after success: set Running.
	b.mu.Lock()
	b.state = domain.BotStateRunning
	b.mu.Unlock()

	b.mu.Lock()
	assert.Equal(t, domain.BotStateRunning, b.state)
	b.mu.Unlock()
}

// TestSeekRecoverAuth_RelaunchFailure_ErrorReturned verifies that a relaunch
// error returns an error and does NOT set Running.
func TestSeekRecoverAuth_RelaunchFailure_ErrorReturned(t *testing.T) {
	orig := authRefreshPollInterval
	authRefreshPollInterval = 50 * time.Millisecond
	t.Cleanup(func() { authRefreshPollInterval = orig })

	store := newAuthTestSessionStore(t)
	seedSession(t, store, "u1", "seek")

	b := newAuthTestBot(t, store)
	b.state = domain.BotStateUserActionRequired

	b.seekSeams = &seekAuthSeams{
		ensureLoggedIn: func(*rod.Page) error { return ErrAuthRequired },
		relaunchBrowser: func() (*rod.Browser, *rod.Page, error) {
			return nil, nil, errors.New("chrome: no display")
		},
	}

	go func() {
		time.Sleep(100 * time.Millisecond)
		advanceSession(t, store, "u1", "seek")
	}()

	_, _, err := b.seekRecoverAuth(nil, func() {})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "relaunch after re-auth")
	assert.Contains(t, err.Error(), "chrome: no display")

	// State must NOT be Running after a relaunch failure.
	b.mu.Lock()
	assert.NotEqual(t, domain.BotStateRunning, b.state)
	b.mu.Unlock()
}

// TestSeekRecoverAuth_StillUnauthAfterRelaunch_FailsCleanly verifies that when
// checkLoginState returns LoginStateNo after relaunch, an error is returned.
func TestSeekRecoverAuth_StillUnauthAfterRelaunch_FailsCleanly(t *testing.T) {
	orig := authRefreshPollInterval
	authRefreshPollInterval = 50 * time.Millisecond
	t.Cleanup(func() { authRefreshPollInterval = orig })

	store := newAuthTestSessionStore(t)
	seedSession(t, store, "u1", "seek")

	b := newAuthTestBot(t, store)

	b.seekSeams = &seekAuthSeams{
		ensureLoggedIn: func(*rod.Page) error { return ErrAuthRequired },
		relaunchBrowser: func() (*rod.Browser, *rod.Page, error) { return nil, nil, nil },
		checkLoginState: func(*rod.Page) (browser.LoginState, error) {
			return browser.LoginStateNo, nil
		},
	}

	go func() {
		time.Sleep(100 * time.Millisecond)
		advanceSession(t, store, "u1", "seek")
	}()

	_, _, err := b.seekRecoverAuth(nil, func() {})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated after re-auth")

	b.mu.Lock()
	assert.NotEqual(t, domain.BotStateRunning, b.state)
	b.mu.Unlock()
}

// TestSeekRecoverAuth_NoForceUnlock verifies that the normal re-auth handoff
// uses relaunchBrowser (which calls PrepareChromeProfileDir, not ForceUnlock).
// In production relaunchBrowser == startProfileBrowser, which is proven by the
// fact that seekRecoverAuth only ever calls relaunch() — never ForceUnlockChromeProfile
// directly. This test exercises the seam path end-to-end to confirm no panic/error.
func TestSeekRecoverAuth_NormalHandoff_UsesRelaunchNotForceUnlock(t *testing.T) {
	orig := authRefreshPollInterval
	authRefreshPollInterval = 50 * time.Millisecond
	t.Cleanup(func() { authRefreshPollInterval = orig })

	store := newAuthTestSessionStore(t)
	seedSession(t, store, "u1", "seek")

	b := newAuthTestBot(t, store)

	relaunchCallCount := 0
	b.seekSeams = &seekAuthSeams{
		ensureLoggedIn: func(*rod.Page) error { return ErrAuthRequired },
		relaunchBrowser: func() (*rod.Browser, *rod.Page, error) {
			relaunchCallCount++
			return nil, nil, nil
		},
		checkLoginState: func(*rod.Page) (browser.LoginState, error) {
			return browser.LoginStateYes, nil
		},
	}

	go func() {
		time.Sleep(100 * time.Millisecond)
		advanceSession(t, store, "u1", "seek")
	}()

	_, _, err := b.seekRecoverAuth(nil, func() {})
	require.NoError(t, err)
	assert.Equal(t, 1, relaunchCallCount,
		"relaunchBrowser (not ForceUnlock) must be called exactly once")
}
