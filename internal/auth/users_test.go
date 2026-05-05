package auth_test

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appdb "github.com/user/jobifai/internal/db"

	"github.com/user/jobifai/internal/auth"
)

func newUsersTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

// ── UserStore ─────────────────────────────────────────────────────────────────

func TestUserStore_Create_ByEmail(t *testing.T) {
	s := auth.NewUserStore(newUsersTestDB(t))

	u, err := s.Create("alice@example.com", "hash", "Alice")
	require.NoError(t, err)
	assert.NotEmpty(t, u.ID)
	assert.Equal(t, "alice@example.com", u.Email)
	assert.Equal(t, "Alice", u.DisplayName)

	got, err := s.ByEmail("alice@example.com")
	require.NoError(t, err)
	assert.Equal(t, u.ID, got.ID)
}

func TestUserStore_Create_DuplicateEmail(t *testing.T) {
	s := auth.NewUserStore(newUsersTestDB(t))

	_, err := s.Create("dup@example.com", "hash", "First")
	require.NoError(t, err)

	_, err = s.Create("dup@example.com", "hash", "Second")
	assert.True(t, errors.Is(err, auth.ErrEmailTaken))
}

func TestUserStore_ByID_NotFound(t *testing.T) {
	s := auth.NewUserStore(newUsersTestDB(t))
	_, err := s.ByID("nonexistent-id")
	assert.True(t, errors.Is(err, auth.ErrUserNotFound))
}

func TestUserStore_ByEmail_NotFound(t *testing.T) {
	s := auth.NewUserStore(newUsersTestDB(t))
	_, err := s.ByEmail("nobody@example.com")
	assert.True(t, errors.Is(err, auth.ErrUserNotFound))
}

func TestUserStore_ByID_Found(t *testing.T) {
	s := auth.NewUserStore(newUsersTestDB(t))

	created, err := s.Create("bob@example.com", "hash", "Bob")
	require.NoError(t, err)

	got, err := s.ByID(created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.ID, got.ID)
	assert.Equal(t, "bob@example.com", got.Email)
}

// ── Refresh Tokens ────────────────────────────────────────────────────────────

func TestRefreshToken_RoundTrip(t *testing.T) {
	db := newUsersTestDB(t)
	s := auth.NewUserStore(db)
	u, err := s.Create("rt@example.com", "hash", "RT User")
	require.NoError(t, err)

	rawToken, err := auth.GenerateRefreshToken()
	require.NoError(t, err)

	require.NoError(t, auth.SaveRefreshToken(db, u.ID, rawToken))

	userID, err := auth.ConsumeRefreshToken(db, rawToken)
	require.NoError(t, err)
	assert.Equal(t, u.ID, userID)
}

func TestRefreshToken_OneTimeUse(t *testing.T) {
	db := newUsersTestDB(t)
	s := auth.NewUserStore(db)
	u, err := s.Create("onetime@example.com", "hash", "One Time")
	require.NoError(t, err)

	rawToken, _ := auth.GenerateRefreshToken()
	require.NoError(t, auth.SaveRefreshToken(db, u.ID, rawToken))

	_, err = auth.ConsumeRefreshToken(db, rawToken)
	require.NoError(t, err)

	// Second consume of the same token must fail.
	_, err = auth.ConsumeRefreshToken(db, rawToken)
	assert.Error(t, err)
}

func TestRefreshToken_Unknown(t *testing.T) {
	db := newUsersTestDB(t)
	_, err := auth.ConsumeRefreshToken(db, "totally-fake-token")
	assert.Error(t, err)
}

func TestRevokeAllRefreshTokens(t *testing.T) {
	db := newUsersTestDB(t)
	s := auth.NewUserStore(db)
	u, err := s.Create("revoke@example.com", "hash", "Revoke")
	require.NoError(t, err)

	tok, _ := auth.GenerateRefreshToken()
	require.NoError(t, auth.SaveRefreshToken(db, u.ID, tok))

	require.NoError(t, auth.RevokeAllRefreshTokens(db, u.ID))

	_, err = auth.ConsumeRefreshToken(db, tok)
	assert.Error(t, err)
}
