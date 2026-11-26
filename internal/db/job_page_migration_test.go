package db_test

import (
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/config"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
	"path/filepath"
	"testing"
)

func TestReaderMigrationPreservesDeployedApplyTasksAndSubmissionHolds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade.db")
	database, err := appdb.Open(path)
	require.NoError(t, err)
	require.NoError(t, goose.DownTo(database, "migrations", 61))
	_, err = database.Exec(`INSERT INTO users(id,email,password_hash) VALUES('reader-upgrade','reader-upgrade@fixture.test','fixture')`)
	require.NoError(t, err)
	_, err = database.Exec(`INSERT INTO apply_url_tasks(id,user_id,url,state,response_json,started_at) VALUES('retained-task','reader-upgrade','https://www.seek.com.au/job/123','interrupted','{"status":"needs_review"}','2026-11-26T00:00:00Z')`)
	require.NoError(t, err)
	_, err = database.Exec(`INSERT INTO apply_attempts(id,user_id,platform,job_id,outcome) VALUES('retained-hold','reader-upgrade','seek','held-job','submission_unknown')`)
	require.NoError(t, err)
	profile := domain.ResumeProfile{Summary: "Retained candidate facts"}
	require.NoError(t, config.NewStore(database).Set("reader-upgrade", "resume_profile", profile))
	require.NoError(t, database.Close())
	upgraded, err := appdb.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = upgraded.Close() })
	var state, response, outcome string
	require.NoError(t, upgraded.QueryRow(`SELECT state,response_json FROM apply_url_tasks WHERE id='retained-task'`).Scan(&state, &response))
	require.Equal(t, "interrupted", state)
	require.JSONEq(t, `{"status":"needs_review"}`, response)
	require.NoError(t, upgraded.QueryRow(`SELECT outcome FROM apply_attempts WHERE id='retained-hold'`).Scan(&outcome))
	require.Equal(t, "submission_unknown", outcome)
	var saved domain.ResumeProfile
	require.NoError(t, config.NewStore(upgraded).Get("reader-upgrade", "resume_profile", &saved))
	require.Equal(t, profile, saved)
	var count int
	require.NoError(t, upgraded.QueryRow(`SELECT COUNT(*) FROM job_page_recipes`).Scan(&count))
	require.Zero(t, count)
}
