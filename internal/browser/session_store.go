package browser

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// SessionStore persists and retrieves platform sessions (encrypted cookies)
// from the platform_sessions SQLite table, scoped per user.
type SessionStore struct {
	db      *sql.DB
	secrets interface {
		Set(userID, key, value string) error
		Get(userID, key string) (string, error)
		Delete(userID, key string) error
	}
}

func NewSessionStore(db *sql.DB, secrets interface {
	Set(userID, key, value string) error
	Get(userID, key string) (string, error)
	Delete(userID, key string) error
}) *SessionStore {
	return &SessionStore{db: db, secrets: secrets}
}

// ErrSessionNotFound is returned when no session exists for a platform.
var ErrSessionNotFound = errors.New("session: not found")

const sessionKeyPrefix = "session:"

// Save encrypts the cookies and upserts the session row for the given user.
func (s *SessionStore) Save(userID, platform, loginMethod string, cookies []Cookie) error {
	raw, err := MarshalCookies(cookies)
	if err != nil {
		return fmt.Errorf("session save: marshal: %w", err)
	}
	// Encrypt into the secrets table under key "session:<platform>"
	if err := s.secrets.Set(userID, sessionKeyPrefix+platform, string(raw)); err != nil {
		return fmt.Errorf("session save: encrypt: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.Exec(
		`INSERT INTO platform_sessions(user_id, platform, cookies_json, login_method, created_at, updated_at)
		 VALUES(?,?,?,?,?,?)
		 ON CONFLICT(user_id, platform) DO UPDATE SET
		   cookies_json=excluded.cookies_json,
		   login_method=excluded.login_method,
		   updated_at=excluded.updated_at`,
		userID, platform, "encrypted", loginMethod, now, now,
	)
	return err
}

// PlatformInfo holds non-sensitive metadata for a stored session.
type PlatformInfo struct {
	Platform    string
	LoginMethod string
	CreatedAt   time.Time
}

// Status returns metadata for the platform session, or ErrSessionNotFound.
func (s *SessionStore) Status(userID, platform string) (*PlatformInfo, error) {
	var p PlatformInfo
	var createdStr string
	err := s.db.QueryRow(
		`SELECT platform, login_method, created_at FROM platform_sessions
		 WHERE user_id = ? AND platform = ?`,
		userID, platform,
	).Scan(&p.Platform, &p.LoginMethod, &createdStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, err
	}
	t, _ := time.Parse(time.RFC3339, createdStr)
	p.CreatedAt = t
	return &p, nil
}

// Load decrypts and returns cookies for a user+platform pair.
func (s *SessionStore) Load(userID, platform string) ([]Cookie, error) {
	if _, err := s.Status(userID, platform); err != nil {
		return nil, err
	}
	raw, err := s.secrets.Get(userID, sessionKeyPrefix+platform)
	if err != nil {
		return nil, fmt.Errorf("session load: decrypt: %w", err)
	}
	return UnmarshalCookies([]byte(raw))
}

// Delete removes the session and its encrypted cookies for the user+platform.
func (s *SessionStore) Delete(userID, platform string) error {
	res, err := s.db.Exec(
		"DELETE FROM platform_sessions WHERE user_id = ? AND platform = ?", userID, platform,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrSessionNotFound
	}
	_ = s.secrets.Delete(userID, sessionKeyPrefix+platform)
	return nil
}
