package bot

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
)

func TestParseScoreBelowThresholdSkip(t *testing.T) {
	t.Parallel()
	score, former, ok := parseScoreBelowThresholdSkip("score 7 < 8")
	require.True(t, ok)
	assert.Equal(t, 7, score)
	assert.Equal(t, 8, former)
	_, _, ok = parseScoreBelowThresholdSkip("blacklisted")
	assert.False(t, ok)
}

func TestReopenScoreSkippedIfEligible_RemovesWhenThresholdLowered(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	userID := "u1"
	_, err := db.Exec(`INSERT INTO jobs_skipped(id,user_id,platform,company,role,link,skip_reason,suitability_score,viewed_at)
		VALUES('job-7',?,'linkedin','Co','Role','http://x','score 7 < 8',7,datetime('now'))`, userID)
	require.NoError(t, err)

	b := New(Config{
		DB:       db,
		UserID:   userID,
		Settings: domain.GeneralSettings{JobSuitabilityScore: 7},
	})
	c := newJobSeenCache()
	_, err = c.load(db, userID)
	require.NoError(t, err)
	b.seenCache = c

	assert.Equal(t, "", b.alreadyAppliedReason("job-7"))
	assert.Equal(t, "", b.alreadyAppliedReason("job-7"))

	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM jobs_skipped WHERE id='job-7'`).Scan(&count))
	assert.Zero(t, count)
}

func TestReopenScoreSkippedIfEligible_StaysSkippedWhenStillBelowThreshold(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	userID := "u1"
	_, err := db.Exec(`INSERT INTO jobs_skipped(id,user_id,platform,company,role,link,skip_reason,suitability_score,viewed_at)
		VALUES('job-6',?,'linkedin','Co','Role','http://x','score 6 < 8',6,datetime('now'))`, userID)
	require.NoError(t, err)

	b := New(Config{DB: db, UserID: userID, Settings: domain.GeneralSettings{JobSuitabilityScore: 7}})
	assert.Equal(t, "in skipped list", b.alreadyAppliedReason("job-6"))
}

func TestReopenScoreSkippedIfEligible_IgnoresNonScoreSkips(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	userID := "u1"
	_, err := db.Exec(`INSERT INTO jobs_skipped(id,user_id,platform,company,role,link,skip_reason,viewed_at)
		VALUES('job-b',?,'linkedin','Co','Role','http://x','blacklisted',datetime('now'))`, userID)
	require.NoError(t, err)

	b := New(Config{DB: db, UserID: userID, Settings: domain.GeneralSettings{JobSuitabilityScore: 7}})
	assert.Equal(t, "in skipped list", b.alreadyAppliedReason("job-b"))
}
