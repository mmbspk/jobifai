package auth_test

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/auth"
)

// ── Password ─────────────────────────────────────────────────────────────────

func TestHashAndCheckPassword(t *testing.T) {
	hash, err := auth.HashPassword("correct-horse-battery-staple")
	require.NoError(t, err)
	assert.NotEmpty(t, hash)

	assert.NoError(t, auth.CheckPassword(hash, "correct-horse-battery-staple"))
	assert.Error(t, auth.CheckPassword(hash, "wrong-password"))
}

func TestHashPassword_Empty(t *testing.T) {
	hash, err := auth.HashPassword("")
	require.NoError(t, err)
	assert.NoError(t, auth.CheckPassword(hash, ""))
	assert.Error(t, auth.CheckPassword(hash, "notempty"))
}

// ── Token ─────────────────────────────────────────────────────────────────────

func TestTokenManager_RoundTrip(t *testing.T) {
	tm := auth.NewTokenManager("test-secret-32-bytes-long-enough!")
	token, err := tm.IssueAccess("user-123", "alice@example.com")
	require.NoError(t, err)
	assert.NotEmpty(t, token)

	claims, err := tm.Verify(token)
	require.NoError(t, err)
	assert.Equal(t, "user-123", claims.UserID)
	assert.Equal(t, "alice@example.com", claims.Email)
}

func TestTokenManager_WrongSecret(t *testing.T) {
	issuer := auth.NewTokenManager("secret-a")
	token, err := issuer.IssueAccess("user-1", "")
	require.NoError(t, err)

	verifier := auth.NewTokenManager("secret-b")
	_, err = verifier.Verify(token)
	assert.Error(t, err)
}

func TestTokenManager_ExpiredToken(t *testing.T) {
	tm := auth.NewTokenManager("test-secret")

	// Manually craft an already-expired token.
	past := time.Now().Add(-2 * time.Hour)
	claims := auth.Claims{
		UserID: "user-expired",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-expired",
			IssuedAt:  jwt.NewNumericDate(past),
			ExpiresAt: jwt.NewNumericDate(past.Add(time.Hour)), // still in past
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte("test-secret"))
	require.NoError(t, err)

	_, err = tm.Verify(signed)
	assert.Error(t, err)
}

func TestTokenManager_EmptyString(t *testing.T) {
	tm := auth.NewTokenManager("test-secret")
	_, err := tm.Verify("")
	assert.Error(t, err)
}

func TestTokenManager_TamperedToken(t *testing.T) {
	tm := auth.NewTokenManager("test-secret")
	token, err := tm.IssueAccess("user-1", "")
	require.NoError(t, err)

	// Flip the last character ensuring it actually changes.
	last := token[len(token)-1]
	replacement := byte('X')
	if last == 'X' {
		replacement = 'Y'
	}
	tampered := token[:len(token)-1] + string(replacement)
	_, err = tm.Verify(tampered)
	assert.Error(t, err)
}

// ── Refresh Token ─────────────────────────────────────────────────────────────

func TestGenerateRefreshToken(t *testing.T) {
	t1, err := auth.GenerateRefreshToken()
	require.NoError(t, err)
	assert.NotEmpty(t, t1)

	t2, err := auth.GenerateRefreshToken()
	require.NoError(t, err)
	assert.NotEqual(t, t1, t2, "each call should produce a unique token")
}

// ── TTL helpers ───────────────────────────────────────────────────────────────

func TestTokenTTLs(t *testing.T) {
	assert.Equal(t, 24*time.Hour, auth.AccessTokenTTL())
	assert.Equal(t, 30*24*time.Hour, auth.RefreshTokenTTL())
}
