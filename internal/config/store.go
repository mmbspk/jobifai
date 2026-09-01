// Package config provides a simple SQLite-backed key/value store for
// persisting application settings as JSON blobs, scoped per user.
package config

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
)

// Store is a SQLite-backed key/value store. Values are JSON-encoded.
// All operations are scoped to a user_id.
type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

var ErrNotFound = fmt.Errorf("config: key not found: %w", domain.ErrNotFound)

// Get decodes the value stored at (userID, key) into dst (must be a pointer).
func (s *Store) Get(userID, key string, dst any) error {
	var raw string
	err := s.db.QueryRow(
		"SELECT value FROM settings WHERE user_id = ? AND key = ?", userID, key,
	).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("config get %q: %w", key, err)
	}
	if err := json.Unmarshal([]byte(raw), dst); err != nil {
		return fmt.Errorf("config unmarshal %q: %w", key, err)
	}
	return nil
}

// Set JSON-encodes src and upserts it at (userID, key).
func (s *Store) Set(userID, key string, src any) error {
	raw, err := json.Marshal(src)
	if err != nil {
		return fmt.Errorf("config marshal %q: %w", key, err)
	}
	_, err = db.ExecWithRetry(s.db,
		`INSERT INTO settings(user_id, key, value) VALUES(?,?,?)
		 ON CONFLICT(user_id, key) DO UPDATE SET value=excluded.value`,
		userID, key, string(raw),
	)
	if err != nil {
		return fmt.Errorf("config set %q: %w", key, err)
	}
	return nil
}
