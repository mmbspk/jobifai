package auth_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/auth"
	appdb "github.com/user/jobifai/internal/db"
)

func TestUserStore_PruneE2ETestUsers(t *testing.T) {
	db, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	users := auth.NewUserStore(db)
	_, err = users.Create("keep@example.com", "hash", "Keep")
	require.NoError(t, err)
	_, err = users.Create("test-1@e2e.test", "hash", "E2E")
	require.NoError(t, err)

	n, err := users.PruneE2ETestUsers()
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	list, err := users.List()
	require.NoError(t, err)
	emails := make([]string, len(list))
	for i, u := range list {
		emails[i] = u.Email
	}
	assert.Contains(t, emails, "keep@example.com")
	assert.Contains(t, emails, "admin@jobifai.local")
	assert.NotContains(t, emails, "test-1@e2e.test")
}

func TestUserStore_DeleteUser_ProtectsDefault(t *testing.T) {
	db, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	users := auth.NewUserStore(db)
	err = users.DeleteUser("__default__")
	assert.ErrorIs(t, err, auth.ErrProtectedUser)
}
