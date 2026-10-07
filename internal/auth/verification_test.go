package auth_test

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/user/jobifai/internal/auth"
	appdb "github.com/user/jobifai/internal/db"
)

func newDBAndUser(t *testing.T) (*sql.DB, string) {
	t.Helper()
	db, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	us := auth.NewUserStore(db)
	user, err := us.Create("tester@example.com", "hash", "Tester")
	require.NoError(t, err)
	return db, user.ID
}

func TestCreateVerificationToken_RoundTrip(t *testing.T) {
	db, userID := newDBAndUser(t)

	rawToken, err := auth.CreateVerificationToken(db, userID)
	require.NoError(t, err)
	assert.NotEmpty(t, rawToken)
	assert.Len(t, rawToken, 44, "base64url of 32 bytes should be 44 chars")
}

func TestConsumeVerificationToken_Success(t *testing.T) {
	db, userID := newDBAndUser(t)

	rawToken, err := auth.CreateVerificationToken(db, userID)
	require.NoError(t, err)

	gotUserID, err := auth.ConsumeVerificationToken(db, rawToken)
	require.NoError(t, err)
	assert.Equal(t, userID, gotUserID)

	// User should now be verified.
	us := auth.NewUserStore(db)
	user, err := us.ByID(userID)
	require.NoError(t, err)
	assert.True(t, user.EmailVerified)
}

func TestConsumeVerificationToken_AlreadyUsed(t *testing.T) {
	db, userID := newDBAndUser(t)

	rawToken, err := auth.CreateVerificationToken(db, userID)
	require.NoError(t, err)

	_, err = auth.ConsumeVerificationToken(db, rawToken)
	require.NoError(t, err)

	// Second call with same token must return ErrTokenAlreadyUsed.
	_, err = auth.ConsumeVerificationToken(db, rawToken)
	require.ErrorIs(t, err, auth.ErrTokenAlreadyUsed)
}

func TestConsumeVerificationToken_NotFound(t *testing.T) {
	db, _ := newDBAndUser(t)

	_, err := auth.ConsumeVerificationToken(db, "not-a-real-token")
	require.ErrorIs(t, err, auth.ErrTokenNotFound)
}

func TestConsumeVerificationToken_Expired(t *testing.T) {
	db, userID := newDBAndUser(t)

	rawToken, err := auth.CreateVerificationToken(db, userID)
	require.NoError(t, err)

	// Expire it by updating expires_at via subquery (SQLite doesn't allow ORDER BY in UPDATE).
	_, err = db.Exec(
		`UPDATE email_verification_tokens SET expires_at = ? WHERE id = (
			SELECT id FROM email_verification_tokens WHERE used_at IS NULL AND user_id = ? ORDER BY created_at DESC LIMIT 1
		)`,
		time.Now().Add(-1*time.Hour), userID,
	)
	require.NoError(t, err)

	_, err = auth.ConsumeVerificationToken(db, rawToken)
	require.ErrorIs(t, err, auth.ErrTokenExpired)
}

func TestConsumeVerificationToken_ConcurrentConsumption(t *testing.T) {
	db, userID := newDBAndUser(t)

	rawToken, err := auth.CreateVerificationToken(db, userID)
	require.NoError(t, err)

	const goroutines = 5
	results := make(chan error, goroutines)
	for range goroutines {
		go func() {
			_, e := auth.ConsumeVerificationToken(db, rawToken)
			results <- e
		}()
	}

	var successes, alreadyUsed int
	for range goroutines {
		switch e := <-results; {
		case e == nil:
			successes++
		case errors.Is(e, auth.ErrTokenAlreadyUsed):
			alreadyUsed++
		default:
			t.Errorf("unexpected error (want nil or ErrTokenAlreadyUsed): %v", e)
		}
	}

	assert.Equal(t, 1, successes, "exactly one goroutine should succeed")
	assert.Equal(t, goroutines-1, alreadyUsed, "all others should get ErrTokenAlreadyUsed")
}

func TestCanResendVerification_AllowsFirstRequest(t *testing.T) {
	db, userID := newDBAndUser(t)

	err := auth.CanResendVerification(db, userID)
	require.NoError(t, err, "first resend should be allowed with no existing tokens")
}

func TestCanResendVerification_RateLimitTooSoon(t *testing.T) {
	db, userID := newDBAndUser(t)

	_, err := auth.CreateVerificationToken(db, userID)
	require.NoError(t, err)

	// Immediate second call should be rate-limited.
	err = auth.CanResendVerification(db, userID)
	require.ErrorIs(t, err, auth.ErrResendTooSoon)
}

func TestCanResendVerification_AllowsAfterWindow(t *testing.T) {
	db, userID := newDBAndUser(t)

	_, err := auth.CreateVerificationToken(db, userID)
	require.NoError(t, err)

	// Back-date the token's created_at beyond the rate-limit window.
	_, err = db.Exec(
		`UPDATE email_verification_tokens SET created_at = ? WHERE user_id = ? AND used_at IS NULL`,
		time.Now().Add(-10*time.Minute), userID,
	)
	require.NoError(t, err)

	err = auth.CanResendVerification(db, userID)
	require.NoError(t, err, "should be allowed after rate-limit window has elapsed")
}

func TestSweepExpiredVerificationTokens_DeletesOnlyExpiredUnused(t *testing.T) {
	db, userID := newDBAndUser(t)

	// Expired unused token — should be deleted.
	_, err := db.Exec(
		`INSERT INTO email_verification_tokens (id, user_id, token_hash, expires_at)
		 VALUES ('exp1', ?, 'hashexp1', ?)`,
		userID, time.Now().Add(-2*time.Hour),
	)
	require.NoError(t, err)

	// Used token (expired) — should NOT be deleted (audit record).
	now := time.Now()
	_, err = db.Exec(
		`INSERT INTO email_verification_tokens (id, user_id, token_hash, expires_at, used_at)
		 VALUES ('used1', ?, 'hashused1', ?, ?)`,
		userID, time.Now().Add(-1*time.Hour), now,
	)
	require.NoError(t, err)

	// Valid (not expired) unused token — should NOT be deleted.
	_, err = db.Exec(
		`INSERT INTO email_verification_tokens (id, user_id, token_hash, expires_at)
		 VALUES ('valid1', ?, 'hashvalid1', ?)`,
		userID, time.Now().Add(24*time.Hour),
	)
	require.NoError(t, err)

	n, err := auth.SweepExpiredVerificationTokens(db)
	require.NoError(t, err)
	assert.Equal(t, int64(1), n, "only the expired unused token should be deleted")

	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM email_verification_tokens`).Scan(&count))
	assert.Equal(t, 2, count, "used and valid tokens should remain")
}
