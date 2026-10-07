package auth_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/auth"
)

func TestCreatePasswordResetToken_RoundTrip(t *testing.T) {
	db := newUsersTestDB(t)
	s := auth.NewUserStore(db)
	u, err := s.Create("reset@example.com", "hash", "Reset User")
	require.NoError(t, err)

	rawToken, err := auth.CreatePasswordResetToken(db, u.ID)
	require.NoError(t, err)
	assert.NotEmpty(t, rawToken)

	newHash, err := auth.HashPassword("newpassword123")
	require.NoError(t, err)
	err = auth.ConsumePasswordResetToken(db, rawToken, newHash)
	require.NoError(t, err)

	updated, err := s.ByID(u.ID)
	require.NoError(t, err)
	require.NoError(t, auth.CheckPassword(updated.PasswordHash, "newpassword123"))
}

func TestConsumePasswordResetToken_AlreadyUsed(t *testing.T) {
	db := newUsersTestDB(t)
	s := auth.NewUserStore(db)
	u, err := s.Create("used@example.com", "hash", "Used")
	require.NoError(t, err)

	rawToken, err := auth.CreatePasswordResetToken(db, u.ID)
	require.NoError(t, err)

	newHash, _ := auth.HashPassword("pass12345")
	require.NoError(t, auth.ConsumePasswordResetToken(db, rawToken, newHash))

	err = auth.ConsumePasswordResetToken(db, rawToken, newHash)
	assert.True(t, errors.Is(err, auth.ErrResetTokenAlreadyUsed))
}

func TestConsumePasswordResetToken_InvalidToken(t *testing.T) {
	db := newUsersTestDB(t)
	err := auth.ConsumePasswordResetToken(db, "totally-fake-token", "somehash")
	assert.True(t, errors.Is(err, auth.ErrResetTokenNotFound))
}

func TestConsumePasswordResetToken_RevokesRefreshTokens(t *testing.T) {
	db := newUsersTestDB(t)
	s := auth.NewUserStore(db)
	u, err := s.Create("revoke@example.com", "hash", "Revoke")
	require.NoError(t, err)

	rawRefresh, err := auth.GenerateRefreshToken()
	require.NoError(t, err)
	require.NoError(t, auth.SaveRefreshToken(db, u.ID, rawRefresh))

	rawToken, err := auth.CreatePasswordResetToken(db, u.ID)
	require.NoError(t, err)
	newHash, _ := auth.HashPassword("pass12345")
	require.NoError(t, auth.ConsumePasswordResetToken(db, rawToken, newHash))

	_, err = auth.ConsumeRefreshToken(db, rawRefresh)
	assert.Error(t, err)
}

func TestConsumePasswordResetToken_Concurrent(t *testing.T) {
	db := newUsersTestDB(t)
	s := auth.NewUserStore(db)
	u, err := s.Create("concurrent-reset@example.com", "hash", "Concurrent")
	require.NoError(t, err)

	rawToken, err := auth.CreatePasswordResetToken(db, u.ID)
	require.NoError(t, err)

	newHash, _ := auth.HashPassword("pass12345")

	const goroutines = 10
	var wg sync.WaitGroup
	var successes, alreadyUsed int
	var mu sync.Mutex

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e := auth.ConsumePasswordResetToken(db, rawToken, newHash)
			mu.Lock()
			defer mu.Unlock()
			if e == nil {
				successes++
			} else if errors.Is(e, auth.ErrResetTokenAlreadyUsed) {
				alreadyUsed++
			}
		}()
	}
	wg.Wait()
	assert.Equal(t, 1, successes, "exactly one goroutine should succeed")
	assert.Equal(t, goroutines-1, alreadyUsed, "all others should get ErrResetTokenAlreadyUsed")
}

func TestCanRequestPasswordReset_RateLimit(t *testing.T) {
	db := newUsersTestDB(t)
	s := auth.NewUserStore(db)
	u, err := s.Create("ratelimit@example.com", "hash", "RateLimit")
	require.NoError(t, err)

	ok, err := auth.CanRequestPasswordReset(db, u.ID)
	require.NoError(t, err)
	assert.True(t, ok, "should be able to request before any token exists")

	_, err = auth.CreatePasswordResetToken(db, u.ID)
	require.NoError(t, err)

	ok, err = auth.CanRequestPasswordReset(db, u.ID)
	require.NoError(t, err)
	assert.False(t, ok, "should be rate-limited after creating a token")
}

func TestCanRequestPasswordReset_UnknownUser(t *testing.T) {
	db := newUsersTestDB(t)
	ok, err := auth.CanRequestPasswordReset(db, "")
	require.NoError(t, err)
	assert.True(t, ok, "empty user ID should always return true (no enumeration)")
}

func TestCreatePasswordResetTokenIfAllowed_RateLimit(t *testing.T) {
	db := newUsersTestDB(t)
	s := auth.NewUserStore(db)
	u, err := s.Create("atomic-ratelimit@example.com", "hash", "AtomicRateLimit")
	require.NoError(t, err)

	token1, err := auth.CreatePasswordResetTokenIfAllowed(db, u.ID)
	require.NoError(t, err)
	assert.NotEmpty(t, token1)

	_, err = auth.CreatePasswordResetTokenIfAllowed(db, u.ID)
	assert.True(t, errors.Is(err, auth.ErrResetRateLimited), "second request should be rate-limited, got: %v", err)
}

func TestCreatePasswordResetTokenIfAllowed_EmptyUserID(t *testing.T) {
	db := newUsersTestDB(t)
	token, err := auth.CreatePasswordResetTokenIfAllowed(db, "")
	require.NoError(t, err)
	assert.Empty(t, token)
}

func TestCreatePasswordResetTokenIfAllowed_Concurrent(t *testing.T) {
	db := newUsersTestDB(t)
	s := auth.NewUserStore(db)
	u, err := s.Create("concurrent-allowed@example.com", "hash", "ConcurrentAllowed")
	require.NoError(t, err)

	const goroutines = 10
	var wg sync.WaitGroup
	var successes, rateLimited int
	var mu sync.Mutex

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := auth.CreatePasswordResetTokenIfAllowed(db, u.ID)
			mu.Lock()
			defer mu.Unlock()
			if e == nil {
				successes++
			} else if errors.Is(e, auth.ErrResetRateLimited) {
				rateLimited++
			}
		}()
	}
	wg.Wait()
	assert.Equal(t, 1, successes, "exactly one goroutine should create a token")
	assert.Equal(t, goroutines-1, rateLimited, "all others should be rate-limited")
}

func TestUpdatePasswordAndRevokeOtherSessions(t *testing.T) {
	db := newUsersTestDB(t)
	s := auth.NewUserStore(db)
	u, err := s.Create("session-revoke@example.com", "hash", "SessionRevoke")
	require.NoError(t, err)

	keepToken, err := auth.GenerateRefreshToken()
	require.NoError(t, err)
	require.NoError(t, auth.SaveRefreshToken(db, u.ID, keepToken))
	otherA, err := auth.GenerateRefreshToken()
	require.NoError(t, err)
	require.NoError(t, auth.SaveRefreshToken(db, u.ID, otherA))
	otherB, err := auth.GenerateRefreshToken()
	require.NoError(t, err)
	require.NoError(t, auth.SaveRefreshToken(db, u.ID, otherB))

	newHash, err := auth.HashPassword("new-password-99")
	require.NoError(t, err)
	require.NoError(t, auth.UpdatePasswordAndRevokeOtherSessions(db, u.ID, newHash, keepToken))

	updated, err := s.ByID(u.ID)
	require.NoError(t, err)
	require.NoError(t, auth.CheckPassword(updated.PasswordHash, "new-password-99"))

	uid, err := auth.ConsumeRefreshToken(db, keepToken)
	assert.NoError(t, err)
	assert.Equal(t, u.ID, uid)

	_, err = auth.ConsumeRefreshToken(db, otherA)
	assert.Error(t, err, "otherA should be revoked")
	_, err = auth.ConsumeRefreshToken(db, otherB)
	assert.Error(t, err, "otherB should be revoked")
}

func TestUpdatePasswordAndRevokeOtherSessions_CrossUserTokenIgnored(t *testing.T) {
	db := newUsersTestDB(t)
	s := auth.NewUserStore(db)
	userA, err := s.Create("session-a@example.com", "hash", "A")
	require.NoError(t, err)
	userB, err := s.Create("session-b@example.com", "hash", "B")
	require.NoError(t, err)

	tokenA, err := auth.GenerateRefreshToken()
	require.NoError(t, err)
	require.NoError(t, auth.SaveRefreshToken(db, userA.ID, tokenA))

	tokenB, err := auth.GenerateRefreshToken()
	require.NoError(t, err)
	require.NoError(t, auth.SaveRefreshToken(db, userB.ID, tokenB))

	newHash, _ := auth.HashPassword("new-pw-99")
	require.NoError(t, auth.UpdatePasswordAndRevokeOtherSessions(db, userA.ID, newHash, tokenB))

	_, err = auth.ConsumeRefreshToken(db, tokenA)
	assert.Error(t, err, "userA's own token must be revoked when cross-user token was supplied")

	uid, err := auth.ConsumeRefreshToken(db, tokenB)
	assert.NoError(t, err)
	assert.Equal(t, userB.ID, uid)
}

func TestSweepExpiredPasswordResetTokens(t *testing.T) {
	db := newUsersTestDB(t)
	s := auth.NewUserStore(db)
	u, err := s.Create("sweep@example.com", "hash", "Sweep")
	require.NoError(t, err)

	_, err = auth.CreatePasswordResetToken(db, u.ID)
	require.NoError(t, err)

	_, err = db.Exec(`UPDATE password_reset_tokens SET expires_at = ? WHERE user_id = ?`,
		time.Now().Add(-2*time.Hour), u.ID)
	require.NoError(t, err)

	require.NoError(t, auth.SweepExpiredPasswordResetTokens(db))

	raw2, err2 := auth.CreatePasswordResetToken(db, u.ID)
	require.NoError(t, err2)
	require.NotEmpty(t, raw2)
}

func TestUpdatePassword(t *testing.T) {
	db := newUsersTestDB(t)
	s := auth.NewUserStore(db)
	u, err := s.Create("update-pw@example.com", "old-hash", "UpdatePW")
	require.NoError(t, err)

	newHash, err := auth.HashPassword("new-password-123")
	require.NoError(t, err)
	require.NoError(t, auth.UpdatePassword(db, u.ID, newHash))

	updated, err := s.ByID(u.ID)
	require.NoError(t, err)
	require.NoError(t, auth.CheckPassword(updated.PasswordHash, "new-password-123"))
}
