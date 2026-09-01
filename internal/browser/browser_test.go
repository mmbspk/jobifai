package browser_test

import (
	"path/filepath"
	"testing"
	"os"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/browser"
	"github.com/user/jobifai/internal/config"
	appdb "github.com/user/jobifai/internal/db"
)

func newTestSessionStore(t *testing.T) *browser.SessionStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return browser.NewSessionStore(db, config.NewSecretsStore(db, "test-passphrase"))
}

func TestMarshalUnmarshalCookies_RoundTrip(t *testing.T) {
	original := []browser.Cookie{
		{Name: "sid", Value: "abc123", Domain: ".example.com", Path: "/"},
		{Name: "tok", Value: "xyz", Domain: "api.example.com", HTTPOnly: true, Secure: true},
	}
	data, err := browser.MarshalCookies(original)
	require.NoError(t, err)

	got, err := browser.UnmarshalCookies(data)
	require.NoError(t, err)
	assert.Equal(t, original, got)
}

func TestUnmarshalCookies_InvalidJSON(t *testing.T) {
	_, err := browser.UnmarshalCookies([]byte("not json"))
	require.Error(t, err)
}

func TestToCookieParams_MapsFields(t *testing.T) {
	cookies := []browser.Cookie{
		{Name: "auth", Value: "token", Domain: ".seek.com.au", Path: "/", HTTPOnly: true},
	}
	params := browser.ToCookieParams(cookies)
	require.Len(t, params, 1)
	assert.Equal(t, "auth", params[0].Name)
	assert.Equal(t, "token", params[0].Value)
	assert.Contains(t, params[0].Domain, "seek.com")
	assert.NotContains(t, params[0].Domain, "seek.com.au", "legacy domain should be rewritten")
}

func TestProfileDir_ConfiguredWins(t *testing.T) {
	assert.Equal(t, "/custom/path", browser.ProfileDir("u1", "seek", "/custom/path"))
}

func TestProfileDir_DefaultPerUserPlatform(t *testing.T) {
	got := browser.ProfileDir("user-abc", "linkedin", "")
	assert.Equal(t, filepath.Join("data", "chrome-profiles", "user-abc", "linkedin"), got)
}

func TestClearStaleChromeProfileLocks_RemovesDeadProcessLock(t *testing.T) {
	dir := t.TempDir()
	// macOS Chrome lock format: hostname-PID
	require.NoError(t, os.Symlink("DR77XGQY46-999999", filepath.Join(dir, "SingletonLock")))
	browser.ClearStaleChromeProfileLocks(dir)
	_, err := os.Lstat(filepath.Join(dir, "SingletonLock"))
	assert.True(t, os.IsNotExist(err))
}

func TestReleaseProfileForLaunch_NoPanicOnEmptyDir(t *testing.T) {
	assert.NotPanics(t, func() { browser.ReleaseProfileForLaunch("") })
}

func TestShouldPersistCookies(t *testing.T) {
	assert.True(t, browser.ShouldPersistCookies(browser.LoginStateYes, nil))
	assert.False(t, browser.ShouldPersistCookies(browser.LoginStateNo, nil))
	assert.False(t, browser.ShouldPersistCookies(browser.LoginStateUnknown, nil))
	assert.False(t, browser.ShouldPersistCookies(browser.LoginStateYes, assert.AnError))
}

func TestSessionStore_Status_NotFound(t *testing.T) {
	store := newTestSessionStore(t)
	_, err := store.Status("user1", "linkedin")
	require.Error(t, err)
	require.ErrorIs(t, err, browser.ErrSessionNotFound)
}

func TestSessionStore_SaveAndStatus(t *testing.T) {
	store := newTestSessionStore(t)
	cookies, _ := browser.MarshalCookies([]browser.Cookie{{Name: "sid", Value: "v", Domain: ".x.com"}})
	require.NoError(t, store.Save("user1", "linkedin", "manual", cookies))

	info, err := store.Status("user1", "linkedin")
	require.NoError(t, err)
	assert.True(t, info.HasSession)
	assert.Equal(t, "manual", string(info.LoginMethod))
}

func TestSessionStore_SaveLoadDelete(t *testing.T) {
	store := newTestSessionStore(t)
	original := []browser.Cookie{{Name: "c", Value: "v", Domain: ".x.com"}}
	data, _ := browser.MarshalCookies(original)
	require.NoError(t, store.Save("user1", "seek", "email_password", data))

	loaded, err := store.Load("user1", "seek")
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.Equal(t, "c", loaded[0].Name)

	require.NoError(t, store.Delete("user1", "seek"))

	_, err = store.Load("user1", "seek")
	require.Error(t, err)
}

func TestSessionStore_Delete_NotFound(t *testing.T) {
	store := newTestSessionStore(t)
	err := store.Delete("user1", "linkedin")
	require.ErrorIs(t, err, browser.ErrSessionNotFound)
}

func TestSessionStore_UserIsolation(t *testing.T) {
	store := newTestSessionStore(t)
	cookies, _ := browser.MarshalCookies([]browser.Cookie{{Name: "a", Value: "b"}})
	require.NoError(t, store.Save("userA", "linkedin", "manual", cookies))

	_, err := store.Status("userB", "linkedin")
	require.ErrorIs(t, err, browser.ErrSessionNotFound)
}
