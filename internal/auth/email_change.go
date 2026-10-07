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
)

// emailChangeTokenTTL matches verificationTokenTTL — 24 h is established convention.
const emailChangeTokenTTL = 24 * time.Hour

var ErrEmailChangeTokenNotFound = errors.New("email change token not found or already used")
var ErrEmailChangeTokenExpired = errors.New("email change token expired")
var ErrEmailChangeTargetTaken = errors.New("email address is already in use by another account")

// CreateEmailChangeToken stores a pending email change for userID.
// Any previous pending change is silently replaced (one pending change per account).
// The raw token is returned and must be embedded in the verification link email.
// Only the SHA-256 hash is persisted.
func CreateEmailChangeToken(db *sql.DB, userID, newEmail string) (string, error) {
	if userID == "" || newEmail == "" {
		return "", fmt.Errorf("email change: userID and newEmail are required")
	}
	raw, err := generateEmailChangeToken()
	if err != nil {
		return "", err
	}
	hash := hashEmailChangeToken(raw)
	expires := time.Now().Add(emailChangeTokenTTL)
	_, err = db.Exec(
		`UPDATE users
		    SET pending_email            = ?,
		        email_change_token_hash  = ?,
		        email_change_expires_at  = ?,
		        updated_at               = ?
		  WHERE id = ?`,
		newEmail, hash, expires, time.Now(), userID,
	)
	if err != nil {
		return "", fmt.Errorf("create email change token: %w", err)
	}
	return raw, nil
}

// ConsumeEmailChangeToken validates rawToken, atomically swaps users.email to
// pending_email, clears all pending-change fields, revokes all refresh tokens,
// and returns the userID, old email, and new email on success.
//
// Uses BEGIN IMMEDIATE so two concurrent calls with the same token produce
// exactly 1 success and all others get ErrEmailChangeTokenNotFound, with
// zero SQLITE_BUSY errors.
//
// On return, all refresh tokens for the user have been revoked. The caller
// should not attempt to preserve any session — users must re-authenticate with
// the new email.
func ConsumeEmailChangeToken(db *sql.DB, rawToken string) (userID, oldEmail, newEmail string, err error) {
	hash := hashEmailChangeToken(rawToken)
	ctx := context.Background()

	conn, err := db.Conn(ctx)
	if err != nil {
		return "", "", "", fmt.Errorf("email change: get conn: %w", err)
	}
	defer func() { _ = conn.Close() }()

	if _, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return "", "", "", fmt.Errorf("email change: begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()

	var expiresAt time.Time
	var pendingEmailNullable sql.NullString
	err = conn.QueryRowContext(ctx,
		`SELECT id, email, pending_email, email_change_expires_at
		   FROM users WHERE email_change_token_hash = ?`,
		hash,
	).Scan(&userID, &oldEmail, &pendingEmailNullable, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", "", ErrEmailChangeTokenNotFound
	}
	if err != nil {
		return "", "", "", fmt.Errorf("email change: lookup token: %w", err)
	}
	if !pendingEmailNullable.Valid {
		// Token hash existed but pending_email was already cleared (double-consume).
		return "", "", "", ErrEmailChangeTokenNotFound
	}
	newEmail = pendingEmailNullable.String

	if time.Now().After(expiresAt) {
		return "", "", "", ErrEmailChangeTokenExpired
	}

	// Re-check that the target email is still available (race with another registration).
	var conflictID string
	checkErr := conn.QueryRowContext(ctx,
		`SELECT id FROM users WHERE email = ? AND id != ?`,
		newEmail, userID,
	).Scan(&conflictID)
	if checkErr != nil && !errors.Is(checkErr, sql.ErrNoRows) {
		return "", "", "", fmt.Errorf("email change: availability check: %w", checkErr)
	}
	if conflictID != "" {
		return "", "", "", ErrEmailChangeTargetTaken
	}

	// Atomically swap email and clear pending-change state.
	// The WHERE clause on email_change_token_hash ensures a concurrent
	// consumer sees RowsAffected == 0 once the first commit NULLs the hash.
	res, execErr := conn.ExecContext(ctx,
		`UPDATE users
		    SET email                   = ?,
		        pending_email           = NULL,
		        email_change_token_hash = NULL,
		        email_change_expires_at = NULL,
		        email_verified          = 1,
		        updated_at              = ?
		  WHERE id = ? AND email_change_token_hash = ?`,
		newEmail, time.Now(), userID, hash,
	)
	if execErr != nil {
		return "", "", "", fmt.Errorf("email change: swap email: %w", execErr)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return "", "", "", ErrEmailChangeTokenNotFound
	}

	// Revoke all refresh tokens — user must re-authenticate with the new email.
	if _, err = conn.ExecContext(ctx,
		`DELETE FROM refresh_tokens WHERE user_id = ?`, userID,
	); err != nil {
		return "", "", "", fmt.Errorf("email change: revoke sessions: %w", err)
	}

	if _, err = conn.ExecContext(ctx, "COMMIT"); err != nil {
		return "", "", "", fmt.Errorf("email change: commit: %w", err)
	}
	committed = true
	return userID, oldEmail, newEmail, nil
}

func generateEmailChangeToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate email change token: %w", err)
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

func hashEmailChangeToken(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("%x", h)
}
