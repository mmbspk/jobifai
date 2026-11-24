package db_test

import (
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/config"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
)

func TestMappingVerificationMigration_RetiresAnswersAndPreservesCandidateHistory(t *testing.T) {
	database, err := appdb.Open(filepath.Join(t.TempDir(), "migration.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	require.NoError(t, goose.DownTo(database, "migrations", 57))
	store := config.NewStore(database)
	profile := domain.ResumeProfile{ApplicationDefaults: domain.ApplicationDefaults{NoticePeriod: "3 weeks"}}
	require.NoError(t, store.Set("candidate", "resume_profile", profile))
	_, err = database.Exec(`INSERT INTO jobs_applied(id,user_id,company,role,platform,link) VALUES('job','candidate','Company','Role','linkedin','https://www.linkedin.com/jobs/view/job')`)
	require.NoError(t, err)
	for _, provenance := range []string{"accepted_by_form", "confirmed_by_reviewer"} {
		_, err = database.Exec(`INSERT INTO form_field_mappings(id,field_sig,user_id,provenance,resolved_value) VALUES(?,?,'candidate',?,'old candidate answer')`, provenance, provenance, provenance)
		require.NoError(t, err)
	}
	require.NoError(t, goose.UpTo(database, "migrations", 58))
	var count int
	require.NoError(t, database.QueryRow(`SELECT COUNT(*) FROM form_field_mappings WHERE provenance='invalidated' AND resolved_value='' AND verified_by='' AND verified_at IS NULL`).Scan(&count))
	assert.Equal(t, 2, count)
	var current domain.ResumeProfile
	require.NoError(t, store.Get("candidate", "resume_profile", &current))
	assert.Equal(t, profile, current)
	require.NoError(t, database.QueryRow(`SELECT COUNT(*) FROM jobs_applied WHERE user_id='candidate' AND id='job'`).Scan(&count))
	assert.Equal(t, 1, count)
	require.NoError(t, goose.DownTo(database, "migrations", 57))
	require.NoError(t, goose.UpTo(database, "migrations", 58))
}
