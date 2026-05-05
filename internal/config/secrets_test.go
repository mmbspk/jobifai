package config_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/user/jobifai/internal/config"
)

func TestSecretsStore_SetGet_RoundTrip(t *testing.T) {
	s := config.NewSecretsStore(newTestDB(t), "test-passphrase")

	require.NoError(t, s.Set("user-1", "llm_api_key", "sk-secret-value"))

	got, err := s.Get("user-1", "llm_api_key")
	require.NoError(t, err)
	assert.Equal(t, "sk-secret-value", got)
}

func TestSecretsStore_GetMissing(t *testing.T) {
	s := config.NewSecretsStore(newTestDB(t), "test-passphrase")

	_, err := s.Get("user-1", "nonexistent")
	assert.True(t, errors.Is(err, config.ErrSecretNotFound))
}

func TestSecretsStore_Has(t *testing.T) {
	s := config.NewSecretsStore(newTestDB(t), "test-passphrase")

	assert.False(t, s.Has("user-1", "k"))
	require.NoError(t, s.Set("user-1", "k", "v"))
	assert.True(t, s.Has("user-1", "k"))
}

func TestSecretsStore_Delete(t *testing.T) {
	s := config.NewSecretsStore(newTestDB(t), "test-passphrase")

	require.NoError(t, s.Set("user-1", "k", "v"))
	require.NoError(t, s.Delete("user-1", "k"))

	_, err := s.Get("user-1", "k")
	assert.True(t, errors.Is(err, config.ErrSecretNotFound))
}

func TestSecretsStore_UserIsolation(t *testing.T) {
	s := config.NewSecretsStore(newTestDB(t), "test-passphrase")

	require.NoError(t, s.Set("user-a", "api_key", "secret-a"))
	require.NoError(t, s.Set("user-b", "api_key", "secret-b"))

	a, err := s.Get("user-a", "api_key")
	require.NoError(t, err)
	b, err := s.Get("user-b", "api_key")
	require.NoError(t, err)

	assert.Equal(t, "secret-a", a)
	assert.Equal(t, "secret-b", b)
}

func TestSecretsStore_IsEncrypted(t *testing.T) {
	db := newTestDB(t)
	s := config.NewSecretsStore(db, "test-passphrase")

	require.NoError(t, s.Set("user-1", "k", "plaintext-value"))

	// Read the raw value from the DB to confirm it is not stored in plaintext.
	var raw string
	require.NoError(t, db.QueryRow("SELECT value FROM secrets WHERE user_id='user-1' AND key='k'").Scan(&raw))
	assert.NotEqual(t, "plaintext-value", raw, "stored value should be encrypted, not plaintext")
}

func TestSecretsStore_Overwrite(t *testing.T) {
	s := config.NewSecretsStore(newTestDB(t), "test-passphrase")

	require.NoError(t, s.Set("user-1", "k", "old"))
	require.NoError(t, s.Set("user-1", "k", "new"))

	got, err := s.Get("user-1", "k")
	require.NoError(t, err)
	assert.Equal(t, "new", got)
}
