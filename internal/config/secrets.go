// Package config also provides an AES-GCM encrypted secrets store backed by
// the same SQLite database, scoped per user.
package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// SecretsStore stores sensitive values encrypted with AES-256-GCM.
// The encryption key is derived from a passphrase via SHA-256.
// All operations are scoped to a user_id.
type SecretsStore struct {
	db  *sql.DB
	key []byte // 32-byte AES-256 key
}

// NewSecretsStore creates a SecretsStore.  passphrase is hashed to produce the
// 32-byte AES key.  For a real production app you'd use a KDF like Argon2;
// SHA-256 is fine here because the passphrase is a random machine key.
func NewSecretsStore(db *sql.DB, passphrase string) *SecretsStore {
	h := sha256.Sum256([]byte(passphrase))
	return &SecretsStore{db: db, key: h[:]}
}

var ErrSecretNotFound = errors.New("secrets: key not found")

// Set encrypts value and upserts it at (userID, key).
func (s *SecretsStore) Set(userID, key, value string) error {
	ct, err := s.encrypt([]byte(value))
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT INTO secrets(user_id, key, value) VALUES(?,?,?)
		 ON CONFLICT(user_id, key) DO UPDATE SET value=excluded.value`,
		userID, key, ct,
	)
	return err
}

// Get decrypts and returns the plaintext value at (userID, key).
func (s *SecretsStore) Get(userID, key string) (string, error) {
	var ct string
	err := s.db.QueryRow(
		"SELECT value FROM secrets WHERE user_id = ? AND key = ?", userID, key,
	).Scan(&ct)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrSecretNotFound
	}
	if err != nil {
		return "", err
	}
	pt, err := s.decrypt(ct)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}

// Has returns true if a non-empty value exists at (userID, key).
func (s *SecretsStore) Has(userID, key string) bool {
	_, err := s.Get(userID, key)
	return err == nil
}

// Delete removes the secret at (userID, key).
func (s *SecretsStore) Delete(userID, key string) error {
	_, err := s.db.Exec("DELETE FROM secrets WHERE user_id = ? AND key = ?", userID, key)
	return err
}

func (s *SecretsStore) encrypt(plain []byte) (string, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", fmt.Errorf("secrets encrypt: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("secrets gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("secrets nonce: %w", err)
	}
	ct := gcm.Seal(nonce, nonce, plain, nil)
	return base64.StdEncoding.EncodeToString(ct), nil
}

func (s *SecretsStore) decrypt(encoded string) ([]byte, error) {
	ct, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("secrets decode: %w", err)
	}
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, fmt.Errorf("secrets cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secrets gcm: %w", err)
	}
	ns := gcm.NonceSize()
	if len(ct) < ns {
		return nil, fmt.Errorf("secrets: ciphertext too short")
	}
	return gcm.Open(nil, ct[:ns], ct[ns:], nil)
}
