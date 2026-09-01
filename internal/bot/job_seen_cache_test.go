package bot

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appdb "github.com/user/jobifai/internal/db"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

func TestJobSeenCache_LoadAndLookup(t *testing.T) {
	db := testDB(t)
	userID := "user-1"

	_, err := db.Exec(`INSERT INTO jobs_applied(id,user_id,platform,company,role,link,applied_at) VALUES('a1',?,'seek','Co','Role','http://x','2026-01-01')`, userID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO jobs_skipped(id,user_id,platform,company,role,link,skip_reason,viewed_at) VALUES('s1',?,'seek','Co','Role','http://y','score','2026-01-01')`, userID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO jobs_pending_review(job_id,user_id,company,role,platform,link,created_at) VALUES('p1',?,'Co','Role','seek','http://z','2026-01-01')`, userID)
	require.NoError(t, err)

	c := newJobSeenCache()
	n, err := c.load(db, userID)
	require.NoError(t, err)
	assert.Equal(t, 3, n)
	assert.Equal(t, "already applied", c.reason("a1"))
	assert.Equal(t, "in skipped list", c.reason("s1"))
	assert.Equal(t, "in Top Matches", c.reason("p1"))
	assert.Equal(t, "", c.reason("totally-unknown-id-xyz"))
}

func TestJobSeenCache_AppliedWinsOverSkipped(t *testing.T) {
	c := newJobSeenCache()
	c.mark("j1", seenApplied)
	c.mark("j1", seenSkipped)
	assert.Equal(t, "already applied", c.reason("j1"))
}
