package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	verificationTokenTTL      = 24 * time.Hour
	resendRateLimitWindow     = 5 * time.Minute
)

// ErrTokenNotFound is returned when a verification token does not exist.
var ErrTokenNotFound = errors.New("verification token not found")

// ErrTokenExpired is returned when a verification token has passed its TTL.
var ErrTokenExpired = errors.New("verification token expired")

// ErrTokenAlreadyUsed is returned when a token has already been consumed.
var ErrTokenAlreadyUsed = errors.New("verification token already used")

// ErrResendTooSoon is returned when another verification email was sent within
// the rate-limit window (resendRateLimitWindow).
var ErrResendTooSoon = errors.New("verification email was sent recently — please wait before requesting another")

// CreateVerificationToken generates a cryptographically secure raw token,
// stores the SHA-256 hash, and returns the raw token for inclusion in email
// links. The raw token is never stored.
func CreateVerificationToken(db *sql.DB, userID string) (rawToken string, err error) {
	raw, err := generateVerificationToken()
	if err != nil {
		return "", err
	}
	hash := hashVerificationToken(raw)
	id := newUUID()
	expires := time.Now().Add(verificationTokenTTL)

	_, err = db.Exec(
		`INSERT INTO email_verification_tokens (id, user_id, token_hash, expires_at)
		 VALUES (?, ?, ?, ?)`,
		id, userID, hash, expires,
	)
	if err != nil {
		return "", fmt.Errorf("create verification token: %w", err)
	}
	return raw, nil
}

// CanResendVerification returns nil if the user is permitted to request a new
// verification email. Returns ErrResendTooSoon if a token was issued within
// the rate-limit window.
func CanResendVerification(db *sql.DB, userID string) error {
	var lastCreated time.Time
	err := db.QueryRow(
		`SELECT created_at FROM email_verification_tokens
		 WHERE user_id = ? AND used_at IS NULL
		 ORDER BY created_at DESC LIMIT 1`,
		userID,
	).Scan(&lastCreated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("check resend rate limit: %w", err)
	}
	if time.Since(lastCreated) < resendRateLimitWindow {
		return ErrResendTooSoon
	}
	return nil
}

// ConsumeVerificationToken validates rawToken, marks the user's email verified,
// and records used_at on the token — all in one transaction to prevent races.
// Returns ErrTokenNotFound / ErrTokenExpired / ErrTokenAlreadyUsed on failure.
func ConsumeVerificationToken(db *sql.DB, rawToken string) (userID string, err error) {
	hash := hashVerificationToken(rawToken)

	tx, err := db.Begin()
	if err != nil {
		return "", fmt.Errorf("verify email: begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var tokenID string
	var expiresAt time.Time
	var usedAt sql.NullTime
	err = tx.QueryRow(
		`SELECT id, user_id, expires_at, used_at
		 FROM email_verification_tokens WHERE token_hash = ?`,
		hash,
	).Scan(&tokenID, &userID, &expiresAt, &usedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrTokenNotFound
	}
	if err != nil {
		return "", fmt.Errorf("verify email: lookup token: %w", err)
	}
	if usedAt.Valid {
		return "", ErrTokenAlreadyUsed
	}
	if time.Now().After(expiresAt) {
		return "", ErrTokenExpired
	}

	// Mark token used.
	if _, err = tx.Exec(
		`UPDATE email_verification_tokens SET used_at = ? WHERE id = ?`,
		time.Now(), tokenID,
	); err != nil {
		return "", fmt.Errorf("verify email: mark token used: %w", err)
	}

	// Mark user's email verified.
	if _, err = tx.Exec(
		`UPDATE users SET email_verified = 1, updated_at = ? WHERE id = ?`,
		time.Now(), userID,
	); err != nil {
		return "", fmt.Errorf("verify email: mark user verified: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return "", fmt.Errorf("verify email: commit: %w", err)
	}
	return userID, nil
}

// SweepExpiredVerificationTokens deletes unused tokens that have exceeded their
// TTL. Used/consumed tokens are preserved as audit records.
// Safe to call on a schedule; returns the number of rows deleted.
func SweepExpiredVerificationTokens(db *sql.DB) (int64, error) {
	res, err := db.Exec(
		`DELETE FROM email_verification_tokens
		 WHERE used_at IS NULL AND expires_at < ?`,
		time.Now(),
	)
	if err != nil {
		return 0, fmt.Errorf("sweep verification tokens: %w", err)
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		log.Info().Int64("deleted", n).Msg("swept expired email verification tokens")
	}
	return n, nil
}

// generateVerificationToken returns a URL-safe base64-encoded random 32-byte token.
func generateVerificationToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate verification token: %w", err)
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

func hashVerificationToken(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("%x", h)
}
