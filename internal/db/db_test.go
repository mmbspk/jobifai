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

// TestOpen_ForeignKeysEnforced verifies that PRAGMA foreign_keys = 1 is active
// on every connection returned by the production opener.  This reproduces the
// bug where _foreign_keys=on (mattn-style) was silently ignored by modernc/sqlite,
// leaving llm_generation_cache rows orphaned after user deletion.
func TestOpen_ForeignKeysEnforced(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "fk.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	// Verify the PRAGMA is on on the first connection the pool hands out.
	var fk int
	require.NoError(t, db.QueryRow("PRAGMA foreign_keys").Scan(&fk))
	assert.Equal(t, 1, fk, "PRAGMA foreign_keys must be 1 after Open; got %d", fk)

	// Insert a parent user row.
	userID := "test-fk-user-id"
	_, err = db.Exec(`INSERT INTO users (id, email, password_hash, is_admin, created_at)
		VALUES (?, 'fk@example.com', 'hash', 0, datetime('now'))`, userID)
	require.NoError(t, err, "insert valid user must succeed")

	// Insert a cache row referencing that user — must succeed.
	_, err = db.Exec(`INSERT INTO llm_generation_cache
		(id, user_id, task, content_fingerprint, visual_identity_hash, state)
		VALUES ('cache-1', ?, 'test-task', 'fp1', '', 'completed')`, userID)
	require.NoError(t, err, "insert cache row with valid user_id must succeed")

	// Insert a cache row referencing a non-existent user — must fail with FK error.
	_, err = db.Exec(`INSERT INTO llm_generation_cache
		(id, user_id, task, content_fingerprint, visual_identity_hash, state)
		VALUES ('cache-2', 'ghost-user', 'test-task', 'fp2', '', 'completed')`)
	require.Error(t, err, "insert cache row with non-existent user_id must fail FK constraint")

	// Delete the user — cache row must cascade.
	_, err = db.Exec(`DELETE FROM users WHERE id = ?`, userID)
	require.NoError(t, err)

	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM llm_generation_cache WHERE user_id = ?`, userID).Scan(&count))
	assert.Zero(t, count, "llm_generation_cache rows must cascade-delete when user is deleted")
}
