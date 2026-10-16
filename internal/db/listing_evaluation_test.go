package db_test

import (
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	appdb "github.com/user/jobifai/internal/db"
	"path/filepath"
	"testing"
)

func TestListingEvaluationMigration_PreservesDocumentsAndDoesNotRetryHolds(t *testing.T) {
	db, err := appdb.Open(filepath.Join(t.TempDir(), "migration.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, goose.DownTo(db, "migrations", 49))
	for _, id := range []string{"unverified", "external", "active", "applied"} {
		hold := "Could not verify the listing."
		if id == "external" {
			hold = "Apply on the employer's website."
		}
		_, err = db.Exec(`INSERT INTO jobs_pending_review(job_id,user_id,company,role,platform,link,document_refs_json) VALUES(?,'u1','Company','Role','linkedin',?,json_object('listing_review_required',json('true'),'hold_reason',?,'resume','prepared.pdf'))`, id, "https://www.linkedin.com/jobs/view/"+id, hold)
		require.NoError(t, err)
	}
	_, err = db.Exec(`INSERT INTO jobs_approved_queue(job_id,user_id,company,role,platform,link) VALUES('active','u1','Company','Role','linkedin','https://www.linkedin.com/jobs/view/active')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO jobs_applied(id,user_id,company,role,platform,link) VALUES('applied','u1','Company','Role','linkedin','https://www.linkedin.com/jobs/view/applied')`)
	require.NoError(t, err)
	require.NoError(t, goose.UpTo(db, "migrations", 50))
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM jobs_skipped`).Scan(&count))
	require.Equal(t, 1, count)
	var code, pack string
	require.NoError(t, db.QueryRow(`SELECT failure_code FROM jobs_skipped WHERE id='unverified' AND user_id='u1'`).Scan(&code))
	require.Equal(t, "listing_verification_required", code)
	require.NoError(t, db.QueryRow(`SELECT document_refs_json FROM jobs_pending_review WHERE job_id='unverified'`).Scan(&pack))
	require.Contains(t, pack, "prepared.pdf")
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM jobs_approved_queue WHERE job_id='unverified'`).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, db.QueryRow(`SELECT needs_evaluation FROM jobs_approved_queue WHERE job_id='active'`).Scan(&count))
	require.Equal(t, 1, count)
}
