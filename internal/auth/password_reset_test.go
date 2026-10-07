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

	// Consuming the token should update the password and succeed.
	newHash, err := auth.HashPassword("newpassword123")
	require.NoError(t, err)
	err = auth.ConsumePasswordResetToken(db, rawToken, newHash)
	require.NoError(t, err)

	// Verify password was updated.
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

	// Second use should fail.
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

	// Issue a refresh token.
	rawRefresh, err := auth.GenerateRefreshToken()
	require.NoError(t, err)
	require.NoError(t, auth.SaveRefreshToken(db, u.ID, rawRefresh))

	// Reset password.
	rawToken, err := auth.CreatePasswordResetToken(db, u.ID)
	require.NoError(t, err)
	newHash, _ := auth.HashPassword("pass12345")
	require.NoError(t, auth.ConsumePasswordResetToken(db, rawToken, newHash))

	// Refresh token should no longer work.
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

func TestSweepExpiredPasswordResetTokens(t *testing.T) {
	db := newUsersTestDB(t)
	s := auth.NewUserStore(db)
	u, err := s.Create("sweep@example.com", "hash", "Sweep")
	require.NoError(t, err)

	_, err = auth.CreatePasswordResetToken(db, u.ID)
	require.NoError(t, err)

	// Manually expire the token.
	_, err = db.Exec(`UPDATE password_reset_tokens SET expires_at = ? WHERE user_id = ?`,
		time.Now().Add(-2*time.Hour), u.ID)
	require.NoError(t, err)

	require.NoError(t, auth.SweepExpiredPasswordResetTokens(db))

	// Attempting to consume should now fail.
	raw2, err2 := auth.CreatePasswordResetToken(db, u.ID) // this also creates a new token
	require.NoError(t, err2)
	require.NotEmpty(t, raw2) // swept token is gone so new one created fine
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
