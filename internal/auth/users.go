// Package auth provides user persistence helpers shared by user handlers and
// the Google OAuth callback.
package auth

import (
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
	sqlite3 "modernc.org/sqlite"
)

// User represents a registered application user.
type User struct {
	ID           string
	Email        string
	DisplayName  string
	PasswordHash string // empty for Google-only accounts
	GoogleID     string // empty for email/password accounts
	AvatarURL    string
	IsAdmin      bool
	CreatedAt    time.Time
}

var ErrUserNotFound = errors.New("user not found")
var ErrEmailTaken = errors.New("email already registered")

// UserStore provides user persistence over SQLite.
type UserStore struct {
	db *sql.DB
}

// NewUserStore creates a UserStore backed by db.
func NewUserStore(db *sql.DB) *UserStore { return &UserStore{db: db} }

// Create inserts a new user and returns it. Returns ErrEmailTaken if the email
// is already registered.
func (s *UserStore) Create(email, passwordHash, displayName string) (*User, error) {
	id := newUUID()
	now := time.Now()
	_, err := s.db.Exec(
		`INSERT INTO users (id, email, password_hash, display_name, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		id, email, passwordHash, displayName, now, now,
	)
	if err != nil {
		if isUniqueConstraint(err) {
			return nil, ErrEmailTaken
		}
		return nil, fmt.Errorf("create user: %w", err)
	}
	return &User{ID: id, Email: email, PasswordHash: passwordHash, DisplayName: displayName, CreatedAt: now}, nil
}

// UpsertGoogle finds or creates a user by their Google ID; updates email/name/avatar on each login.
func (s *UserStore) UpsertGoogle(googleID, email, displayName, avatarURL string) (*User, error) {
	now := time.Now()
	// Try to find existing user by google_id or email.
	u, err := s.ByGoogleID(googleID)
	if err == nil {
		// Update profile fields.
		if _, err := s.db.Exec(
			`UPDATE users SET email=?, display_name=?, avatar_url=?, updated_at=? WHERE id=?`,
			email, displayName, avatarURL, now, u.ID,
		); err != nil {
			log.Error().Err(err).Str("user_id", u.ID).Msg("upsert google: failed to update existing user")
		}
		u.Email = email
		u.DisplayName = displayName
		u.AvatarURL = avatarURL
		return u, nil
	}
	// Maybe the user registered by email first, link the Google ID.
	u, err = s.ByEmail(email)
	if err == nil {
		if _, err := s.db.Exec(
			`UPDATE users SET google_id=?, display_name=?, avatar_url=?, updated_at=? WHERE id=?`,
			googleID, displayName, avatarURL, now, u.ID,
		); err != nil {
			log.Error().Err(err).Str("user_id", u.ID).Msg("upsert google: failed to link google id")
		}
		u.GoogleID = googleID
		u.DisplayName = displayName
		u.AvatarURL = avatarURL
		return u, nil
	}
	// New user, create one.
	id := newUUID()
	_, err = s.db.Exec(
		`INSERT INTO users (id, email, google_id, display_name, avatar_url, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, email, googleID, displayName, avatarURL, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("upsert google user: %w", err)
	}
	return &User{ID: id, Email: email, GoogleID: googleID, DisplayName: displayName, AvatarURL: avatarURL, CreatedAt: now}, nil
}

// ByID fetches a user by their primary key.
func (s *UserStore) ByID(id string) (*User, error) {
	return s.scan(s.db.QueryRow(
		`SELECT id, email, COALESCE(password_hash,''), display_name,
		        COALESCE(google_id,''), COALESCE(avatar_url,''), is_admin, created_at
		 FROM users WHERE id = ?`, id,
	))
}

// ByEmail fetches a user by email.
func (s *UserStore) ByEmail(email string) (*User, error) {
	return s.scan(s.db.QueryRow(
		`SELECT id, email, COALESCE(password_hash,''), display_name,
		        COALESCE(google_id,''), COALESCE(avatar_url,''), is_admin, created_at
		 FROM users WHERE email = ?`, email,
	))
}

// ByGoogleID fetches a user by their Google subject ID.
func (s *UserStore) ByGoogleID(googleID string) (*User, error) {
	return s.scan(s.db.QueryRow(
		`SELECT id, email, COALESCE(password_hash,''), display_name,
		        COALESCE(google_id,''), COALESCE(avatar_url,''), is_admin, created_at
		 FROM users WHERE google_id = ?`, googleID,
	))
}

// Update applies display_name and/or avatar_url changes.
func (s *UserStore) Update(userID, displayName, avatarURL string) error {
	_, err := s.db.Exec(
		`UPDATE users SET display_name=?, avatar_url=?, updated_at=? WHERE id=?`,
		displayName, avatarURL, time.Now(), userID,
	)
	return err
}

func (s *UserStore) scan(row *sql.Row) (*User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.DisplayName, &u.GoogleID, &u.AvatarURL, &u.IsAdmin, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	return &u, err
}

// ─── Refresh token helpers ────────────────────────────────────────────────────

// SaveRefreshToken persists a new refresh token for the given user.
func SaveRefreshToken(db *sql.DB, userID, rawToken string) error {
	hash := hashRefreshToken(rawToken)
	expires := time.Now().Add(RefreshTokenTTL())
	id := newUUID()
	_, err := db.Exec(
		`INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at) VALUES (?, ?, ?, ?)`,
		id, userID, hash, expires,
	)
	return err
}

// ConsumeRefreshToken validates a raw refresh token, returns user_id, and
// deletes the token so it can only be used once.
func ConsumeRefreshToken(db *sql.DB, rawToken string) (string, error) {
	hash := hashRefreshToken(rawToken)
	var userID string
	var expiresAt time.Time
	err := db.QueryRow(
		`SELECT user_id, expires_at FROM refresh_tokens WHERE token_hash = ?`, hash,
	).Scan(&userID, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errors.New("invalid refresh token")
	}
	if err != nil {
		return "", err
	}
	if time.Now().After(expiresAt) {
		_, _ = db.Exec(`DELETE FROM refresh_tokens WHERE token_hash = ?`, hash)
		return "", errors.New("refresh token expired")
	}
	_, _ = db.Exec(`DELETE FROM refresh_tokens WHERE token_hash = ?`, hash)
	return userID, nil
}

// RevokeAllRefreshTokens deletes all refresh tokens for a user (used on logout).
func RevokeAllRefreshTokens(db *sql.DB, userID string) error {
	_, err := db.Exec(`DELETE FROM refresh_tokens WHERE user_id = ?`, userID)
	return err
}

func hashRefreshToken(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("%x", h)
}

// isUniqueConstraint returns true for SQLite unique constraint violations.
func isUniqueConstraint(err error) bool {
	// SQLITE_CONSTRAINT_UNIQUE = 2067 (modernc.org/sqlite extended error code)
	var sqlErr *sqlite3.Error
	return errors.As(err, &sqlErr) && sqlErr.Code() == 2067
}
