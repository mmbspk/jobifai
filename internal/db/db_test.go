package db_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appdb "github.com/user/jobifai/internal/db"
)

func TestOpen_AppliesMigrations(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	var tableName string
	err = db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='users'`).Scan(&tableName)
	require.NoError(t, err)
	assert.Equal(t, "users", tableName)

	var versionID int64
	err = db.QueryRow(`SELECT version_id FROM goose_db_version ORDER BY id DESC LIMIT 1`).Scan(&versionID)
	require.NoError(t, err)
	assert.Positive(t, versionID)
}
