package config_test

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appdb "github.com/user/jobifai/internal/db"

	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/domain"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

func TestStore_GetMissing(t *testing.T) {
	s := config.NewStore(newTestDB(t))
	var dst domain.GeneralSettings
	err := s.Get("user-1", "general_settings", &dst)
	assert.True(t, errors.Is(err, config.ErrNotFound))
}

func TestStore_SetGet_RoundTrip(t *testing.T) {
	s := config.NewStore(newTestDB(t))

	gs := domain.GeneralSettings{
		JobSuitabilityScore: 7,
		HalalJobFilter:      true,
	}
	require.NoError(t, s.Set("user-1", "general_settings", gs))

	var got domain.GeneralSettings
	require.NoError(t, s.Get("user-1", "general_settings", &got))
	assert.Equal(t, 7, got.JobSuitabilityScore)
	assert.True(t, got.HalalJobFilter)
}

func TestStore_Overwrite(t *testing.T) {
	s := config.NewStore(newTestDB(t))

	require.NoError(t, s.Set("user-1", "k", map[string]int{"v": 1}))
	require.NoError(t, s.Set("user-1", "k", map[string]int{"v": 99}))

	var got map[string]int
	require.NoError(t, s.Get("user-1", "k", &got))
	assert.Equal(t, 99, got["v"])
}

func TestStore_UserIsolation(t *testing.T) {
	s := config.NewStore(newTestDB(t))

	require.NoError(t, s.Set("user-a", "key", "value-a"))
	require.NoError(t, s.Set("user-b", "key", "value-b"))

	var a, b string
	require.NoError(t, s.Get("user-a", "key", &a))
	require.NoError(t, s.Get("user-b", "key", &b))
	assert.Equal(t, "value-a", a)
	assert.Equal(t, "value-b", b)
}
