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

func TestApplyEngineSchema_RepairsMarkedMigrationAndPreservesHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing.db")
	database, err := appdb.Open(path)
	require.NoError(t, err)
	latestVersion, err := goose.GetDBVersion(database)
	require.NoError(t, err)
	require.NoError(t, goose.DownTo(database, "migrations", 59))
	_, err = database.Exec(`DROP TABLE apply_engine_audit`)
	require.NoError(t, err)
	_, err = database.Exec(`ALTER TABLE apply_attempts DROP COLUMN engine`)
	require.NoError(t, err)
	_, err = database.Exec(`INSERT INTO apply_attempts(id,user_id,platform,job_id,outcome,external_identity_json) VALUES('retained-attempt','candidate','seek','retained-job','submitted_confirmed','{"tenant":"retained"}')`)
	require.NoError(t, err)
	_, err = database.Exec(`INSERT INTO jobs_applied(id,user_id,company,role,platform,link) VALUES('retained-job','candidate','Company','Role','seek','https://www.seek.com.au/job/123')`)
	require.NoError(t, err)
	profile := domain.ResumeProfile{ApplicationDefaults: domain.ApplicationDefaults{NoticePeriod: "3 weeks"}}
	require.NoError(t, config.NewStore(database).Set("candidate", "resume_profile", profile))
	require.NoError(t, database.Close())

	repaired, err := appdb.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = repaired.Close() })
	var version int64
	require.NoError(t, repaired.QueryRow(`SELECT MAX(version_id) FROM goose_db_version WHERE is_applied=1`).Scan(&version))
	assert.EqualValues(t, latestVersion, version)
	var engine, outcome, external string
	require.NoError(t, repaired.QueryRow(`SELECT engine,outcome,external_identity_json FROM apply_attempts WHERE id='retained-attempt'`).Scan(&engine, &outcome, &external))
	assert.Empty(t, engine)
	assert.Equal(t, "submitted_confirmed", outcome)
	assert.JSONEq(t, `{"tenant":"retained"}`, external)
	var count int
	require.NoError(t, repaired.QueryRow(`SELECT COUNT(*) FROM jobs_applied WHERE id='retained-job'`).Scan(&count))
	assert.Equal(t, 1, count)
	var current domain.ResumeProfile
	require.NoError(t, config.NewStore(repaired).Get("candidate", "resume_profile", &current))
	assert.Equal(t, profile, current)
	_, err = repaired.Exec(`INSERT INTO apply_engine_audit(id,admin_id,old_mode,new_mode,reason) VALUES('new-audit','candidate','legacy','adaptive','pilot')`)
	require.NoError(t, err)
}

func TestApplyEngineSchema_ReplayPreservesExistingAuditAndEngine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "normal.db")
	database, err := appdb.Open(path)
	require.NoError(t, err)
	_, err = database.Exec(`INSERT INTO apply_engine_audit(id,admin_id,old_mode,new_mode,reason,config_json) VALUES('audit','candidate','legacy','adaptive','pilot','{"mode":"adaptive"}')`)
	require.NoError(t, err)
	_, err = database.Exec(`INSERT INTO apply_attempts(id,user_id,platform,engine) VALUES('attempt','candidate','linkedin','adaptive')`)
	require.NoError(t, err)
	require.NoError(t, goose.DownTo(database, "migrations", 59))
	require.NoError(t, goose.UpTo(database, "migrations", 60))
	require.NoError(t, database.Close())
	reopened, err := appdb.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reopened.Close() })
	var engine, configJSON string
	require.NoError(t, reopened.QueryRow(`SELECT engine FROM apply_attempts WHERE id='attempt'`).Scan(&engine))
	assert.Equal(t, "adaptive", engine)
	require.NoError(t, reopened.QueryRow(`SELECT config_json FROM apply_engine_audit WHERE id='audit'`).Scan(&configJSON))
	assert.JSONEq(t, `{"mode":"adaptive"}`, configJSON)
}
