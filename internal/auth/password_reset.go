package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
)

const passwordResetTokenTTL = 1 * time.Hour

var ErrResetTokenNotFound = errors.New("password reset token not found or expired")
var ErrResetTokenAlreadyUsed = errors.New("password reset token has already been used")

// GeneratePasswordResetToken returns a cryptographically random 32-byte base64url token.
func GeneratePasswordResetToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate reset token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashPasswordResetToken(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("%x", h)
}

// CanRequestPasswordReset returns true when the user has not created a valid
// (unused) reset token in the last 5 minutes. Returns true for unknown users
// so the caller can still return 202 without leaking account existence.
func CanRequestPasswordReset(db *sql.DB, userID string) (bool, error) {
	if userID == "" {
		return true, nil
	}
	var lastCreated time.Time
	err := db.QueryRow(
		`SELECT created_at FROM password_reset_tokens
		 WHERE user_id = ? AND used_at IS NULL
		 ORDER BY created_at DESC LIMIT 1`,
		userID,
	).Scan(&lastCreated)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("reset rate check: %w", err)
	}
	return time.Since(lastCreated) >= 5*time.Minute, nil
}

// CreatePasswordResetToken inserts a new reset token for userID and returns
// the raw token. Only the SHA-256 hash is persisted.
func CreatePasswordResetToken(db *sql.DB, userID string) (string, error) {
	rawToken, err := GeneratePasswordResetToken()
	if err != nil {
		return "", err
	}
	hash := hashPasswordResetToken(rawToken)
	expires := time.Now().Add(passwordResetTokenTTL)
	id := newUUID()
	_, err = db.Exec(
		`INSERT INTO password_reset_tokens (id, user_id, token_hash, expires_at)
		 VALUES (?, ?, ?, ?)`,
		id, userID, hash, expires,
	)
	if err != nil {
		return "", fmt.Errorf("create reset token: %w", err)
	}
	return rawToken, nil
}

// ConsumePasswordResetToken validates and single-use-marks a reset token, then
// atomically updates the user's password and revokes all refresh tokens so all
// existing sessions are invalidated.
// Uses BEGIN IMMEDIATE for race-safe single-use enforcement.
func ConsumePasswordResetToken(db *sql.DB, rawToken, newPasswordHash string) error {
	ctx := context.Background()
	hash := hashPasswordResetToken(rawToken)

	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("consume reset token: conn: %w", err)
	}
	defer func() { _ = conn.Close() }()

	if _, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("consume reset token: begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			if _, rbErr := conn.ExecContext(context.Background(), "ROLLBACK"); rbErr != nil {
				log.Error().Err(rbErr).Msg("consume reset token: rollback failed")
			}
		}
	}()

	var tokenID, userID string
	var expiresAt time.Time
	var usedAt sql.NullTime
	err = conn.QueryRowContext(ctx,
		`SELECT id, user_id, expires_at, used_at FROM password_reset_tokens WHERE token_hash = ?`, hash,
	).Scan(&tokenID, &userID, &expiresAt, &usedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrResetTokenNotFound
	}
	if err != nil {
		return fmt.Errorf("consume reset token: fetch: %w", err)
	}
	if usedAt.Valid {
		return ErrResetTokenAlreadyUsed
	}
	if time.Now().After(expiresAt) {
		return ErrResetTokenNotFound
	}

	res, err := conn.ExecContext(ctx,
		`UPDATE password_reset_tokens SET used_at = datetime('now') WHERE id = ? AND used_at IS NULL`, tokenID,
	)
	if err != nil {
		return fmt.Errorf("consume reset token: mark used: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrResetTokenAlreadyUsed
	}

	if _, err = conn.ExecContext(ctx,
		`UPDATE users SET password_hash = ?, updated_at = datetime('now') WHERE id = ?`,
		newPasswordHash, userID,
	); err != nil {
		return fmt.Errorf("consume reset token: update password: %w", err)
	}

	if _, err = conn.ExecContext(ctx,
		`DELETE FROM refresh_tokens WHERE user_id = ?`, userID,
	); err != nil {
		return fmt.Errorf("consume reset token: revoke sessions: %w", err)
	}

	if _, err = conn.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("consume reset token: commit: %w", err)
	}
	committed = true
	return nil
}

// SweepExpiredPasswordResetTokens removes expired unused tokens.
// Safe to call periodically from a background goroutine.
func SweepExpiredPasswordResetTokens(db *sql.DB) error {
	_, err := db.Exec(
		`DELETE FROM password_reset_tokens WHERE expires_at < datetime('now') AND used_at IS NULL`,
	)
	return err
}

// UpdatePassword sets a new bcrypt password hash for the user. Does NOT
// revoke existing refresh tokens — callers that need session invalidation
// should call RevokeAllRefreshTokens separately.
func UpdatePassword(db *sql.DB, userID, newPasswordHash string) error {
	_, err := db.Exec(
		`UPDATE users SET password_hash = ?, updated_at = datetime('now') WHERE id = ?`,
		newPasswordHash, userID,
	)
	return err
}
