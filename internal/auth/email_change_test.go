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

// openEmailChangeDB returns a DB with migrations applied and two users created:
// one with only email/password auth (ownerID) and one as a conflict target.
func openEmailChangeDB(t *testing.T) (*sql.DB, *auth.UserStore, string) {
	t.Helper()
	d, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	us := auth.NewUserStore(d)
	owner, err := us.Create("owner@example.com", "hash", "Owner")
	require.NoError(t, err)
	return d, us, owner.ID
}

func TestCreateEmailChangeToken_RoundTrip(t *testing.T) {
	db, _, ownerID := openEmailChangeDB(t)

	rawToken, err := auth.CreateEmailChangeToken(db, ownerID, "new@example.com")
	require.NoError(t, err)
	assert.NotEmpty(t, rawToken)
}

func TestConsumeEmailChangeToken_Success(t *testing.T) {
	db, us, ownerID := openEmailChangeDB(t)

	rawToken, err := auth.CreateEmailChangeToken(db, ownerID, "new@example.com")
	require.NoError(t, err)

	gotUID, oldEmail, newEmail, err := auth.ConsumeEmailChangeToken(db, rawToken)
	require.NoError(t, err)
	assert.Equal(t, ownerID, gotUID)
	assert.Equal(t, "owner@example.com", oldEmail)
	assert.Equal(t, "new@example.com", newEmail)

	// User email must be updated.
	user, err := us.ByID(ownerID)
	require.NoError(t, err)
	assert.Equal(t, "new@example.com", user.Email)
	assert.Empty(t, user.PendingEmail)
}

func TestConsumeEmailChangeToken_NotFound(t *testing.T) {
	db, _, _ := openEmailChangeDB(t)

	_, _, _, err := auth.ConsumeEmailChangeToken(db, "not-a-real-token")
	require.ErrorIs(t, err, auth.ErrEmailChangeTokenNotFound)
}

func TestConsumeEmailChangeToken_AlreadyUsed(t *testing.T) {
	db, _, ownerID := openEmailChangeDB(t)

	rawToken, err := auth.CreateEmailChangeToken(db, ownerID, "new@example.com")
	require.NoError(t, err)

	_, _, _, err = auth.ConsumeEmailChangeToken(db, rawToken)
	require.NoError(t, err)

	// Second consumption must fail.
	_, _, _, err = auth.ConsumeEmailChangeToken(db, rawToken)
	require.ErrorIs(t, err, auth.ErrEmailChangeTokenNotFound)
}

func TestConsumeEmailChangeToken_Expired(t *testing.T) {
	db, _, ownerID := openEmailChangeDB(t)

	rawToken, err := auth.CreateEmailChangeToken(db, ownerID, "new@example.com")
	require.NoError(t, err)

	// Back-date the expiry.
	_, err = db.Exec(
		`UPDATE users SET email_change_expires_at = ? WHERE id = ?`,
		time.Now().Add(-1*time.Hour), ownerID,
	)
	require.NoError(t, err)

	_, _, _, err = auth.ConsumeEmailChangeToken(db, rawToken)
	require.ErrorIs(t, err, auth.ErrEmailChangeTokenExpired)
}

func TestConsumeEmailChangeToken_TargetTaken(t *testing.T) {
	db, us, ownerID := openEmailChangeDB(t)

	// Create a second account that already holds the target address.
	_, err := us.Create("other@example.com", "hash2", "Other")
	require.NoError(t, err)

	rawToken, err := auth.CreateEmailChangeToken(db, ownerID, "other@example.com")
	require.NoError(t, err)

	_, _, _, err = auth.ConsumeEmailChangeToken(db, rawToken)
	require.ErrorIs(t, err, auth.ErrEmailChangeTargetTaken)
}

func TestConsumeEmailChangeToken_RevokesRefreshTokens(t *testing.T) {
	db, _, ownerID := openEmailChangeDB(t)

	// Insert a refresh token for the owner.
	rawRefresh, err := auth.GenerateRefreshToken()
	require.NoError(t, err)
	require.NoError(t, auth.SaveRefreshToken(db, ownerID, rawRefresh))

	rawToken, err := auth.CreateEmailChangeToken(db, ownerID, "new@example.com")
	require.NoError(t, err)

	_, _, _, err = auth.ConsumeEmailChangeToken(db, rawToken)
	require.NoError(t, err)

	// Trying to consume the refresh token must fail now.
	_, err = auth.ConsumeRefreshToken(db, rawRefresh)
	assert.Error(t, err, "refresh token should be revoked after email change")
}

func TestConsumeEmailChangeToken_ReplacesPreviousPending(t *testing.T) {
	db, us, ownerID := openEmailChangeDB(t)

	// First change request.
	_, err := auth.CreateEmailChangeToken(db, ownerID, "first@example.com")
	require.NoError(t, err)

	// Second request replaces the first.
	rawToken2, err := auth.CreateEmailChangeToken(db, ownerID, "second@example.com")
	require.NoError(t, err)

	// Only the second token must succeed.
	_, _, newEmail, err := auth.ConsumeEmailChangeToken(db, rawToken2)
	require.NoError(t, err)
	assert.Equal(t, "second@example.com", newEmail)

	user, err := us.ByID(ownerID)
	require.NoError(t, err)
	assert.Equal(t, "second@example.com", user.Email)
}

func TestConsumeEmailChangeToken_ConcurrentConsumption(t *testing.T) {
	db, _, ownerID := openEmailChangeDB(t)

	rawToken, err := auth.CreateEmailChangeToken(db, ownerID, "concurrent@example.com")
	require.NoError(t, err)

	const goroutines = 5
	type result struct{ err error }
	results := make(chan result, goroutines)
	for range goroutines {
		go func() {
			_, _, _, e := auth.ConsumeEmailChangeToken(db, rawToken)
			results <- result{e}
		}()
	}

	var successes, notFound int
	for range goroutines {
		r := <-results
		switch {
		case r.err == nil:
			successes++
		case errors.Is(r.err, auth.ErrEmailChangeTokenNotFound):
			notFound++
		default:
			t.Errorf("unexpected error: %v", r.err)
		}
	}

	assert.Equal(t, 1, successes, "exactly one goroutine should succeed")
	assert.Equal(t, goroutines-1, notFound, "all others should get ErrEmailChangeTokenNotFound")
}
