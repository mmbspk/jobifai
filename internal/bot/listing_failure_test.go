package bot

import (
	"context"
	"errors"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"strings"
	"testing"
)

func TestListingEvaluation_ServiceErrorsPersistWithoutQualification(t *testing.T) {
	for _, platform := range []domain.Platform{domain.PlatformLinkedIn, domain.PlatformSeek} {
		for _, stage := range []string{"score", "halal"} {
			t.Run(string(platform)+"/"+stage, func(t *testing.T) {
				db := testDB(t)
				cfg := Config{DB: db, UserID: "u1", Platform: platform, Scorer: stubScorer{result: domain.JobScore{Score: 9}}}
				if stage == "score" {
					cfg.Scorer = stubScorer{err: errors.New("service unavailable")}
				} else {
					cfg.HalalChecker = stubHalal{err: errors.New("service unavailable")}
				}
				b := New(cfg)
				job := linkedInJob{ID: "123", URL: "https://www.linkedin.com/jobs/view/123", Title: "Role", Company: "Company"}
				var ok bool
				if platform == domain.PlatformSeek {
					_, _, _, ok = b.checkSeekScore(context.Background(), seekJob{ID: job.ID, URL: job.URL, Title: job.Title, Company: job.Company}, "actual description")
				} else {
					_, _, _, ok = b.checkScore(context.Background(), job, "actual description")
				}
				require.False(t, ok)
				var code string
				require.NoError(t, db.QueryRow(`SELECT failure_code FROM jobs_skipped WHERE id='123' AND user_id='u1'`).Scan(&code))
				require.Equal(t, "evaluation_failed", code)
				var count int
				require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM jobs_pending_review`).Scan(&count))
				require.Zero(t, count)
			})
		}
	}
}

func TestListingEvaluation_RequiresDescriptionAndScorer(t *testing.T) {
	require.ErrorIs(t, listingEvaluationError("Role at Company", stubScorer{}), errJobDescriptionMissing)
	require.ErrorIs(t, listingEvaluationError(strings.Repeat("Responsibilities ", 20), nil), errJobScorerUnavailable)
	require.NoError(t, listingEvaluationError(strings.Repeat("Responsibilities ", 20), stubScorer{}))
	b := New(Config{})
	score, _, _, ok := b.checkScore(context.Background(), linkedInJob{}, "description")
	require.Zero(t, score)
	require.False(t, ok)
	score, _, _, ok = b.checkSeekScore(context.Background(), seekJob{}, "description")
	require.Zero(t, score)
	require.False(t, ok)
}

func TestListingFailures_StopOnlyConsecutiveDistinctFailures(t *testing.T) {
	b := New(Config{})
	b.noteListingFailure("a", "identity")
	b.noteListingFailure("a", "identity")
	b.noteListingFailure("b", "identity")
	require.NotEqual(t, domain.BotStateError, b.State())
	b.noteListingHealthy()
	b.noteListingFailure("c", "identity")
	b.noteListingFailure("d", "description")
	b.noteListingFailure("e", "description")
	require.NotEqual(t, domain.BotStateError, b.State())
	b.noteListingFailure("f", "description")
	require.Equal(t, domain.BotStateError, b.State())
	select {
	case <-b.stopCh:
	default:
		t.Fatal("repeated failures must stop the run")
	}
}

func TestMarkAttemptSubmitDispatch_DurableAndUserScoped(t *testing.T) {
	db := testDB(t)
	ac := newAttemptContext("u1", "linkedin", "123", "https://www.linkedin.com/jobs/view/123", "Role", "Company", "test", "")
	beginAttempt(db, ac)
	require.Error(t, markAttemptSubmitDispatch(db, ac.AttemptID, "u2"))
	require.NoError(t, markAttemptSubmitDispatch(db, ac.AttemptID, "u1"))
	var stage, outcome string
	require.NoError(t, db.QueryRow(`SELECT stage,outcome FROM apply_attempts WHERE id=?`, ac.AttemptID).Scan(&stage, &outcome))
	require.Equal(t, "submit_dispatch", stage)
	require.Equal(t, string(OutcomeInProgress), outcome)
}

func TestEvaluationRetry_UnknownSubmissionCannotReachBrowser(t *testing.T) {
	db := testDB(t)
	b := New(Config{DB: db, UserID: "u1", Platform: domain.PlatformLinkedIn})
	j := approvedJob{JobID: "123", Platform: "linkedin", Link: "https://www.linkedin.com/jobs/view/123", NeedsEvaluation: true}
	ac := newAttemptContext("u1", "linkedin", j.JobID, j.Link, "Role", "Company", "test", "")
	beginAttempt(db, ac)
	finishAttempt(db, ac, OutcomeSubmissionUnknown, "confirmation", "unknown", "")
	for _, variant := range []string{"https://www.linkedin.com/jobs/view/123/", "https://www.linkedin.com/jobs/view/123?trackingId=example", "https://linkedin.com/jobs/view/123/"} {
		require.Error(t, unresolvedSubmission(db, "u1", "new-manual-id", variant, "new-attempt"))
	}
	require.NoError(t, unresolvedSubmission(db, "u1", "other-id", "https://other.example/jobs/view/123", ""))
	require.False(t, b.processEvaluationRetry(context.Background(), nil, j))
}

func TestUnresolvedSubmission_BlocksOtherAttemptsAndMatchesURL(t *testing.T) {
	db := testDB(t)
	ac := newAttemptContext("u1", "linkedin", "legacy-id", "https://www.linkedin.com/jobs/view/123", "Role", "Company", "test", "")
	beginAttempt(db, ac)
	require.NoError(t, unresolvedSubmission(db, "u1", ac.JobID, ac.URL, ac.AttemptID))
	require.NoError(t, unresolvedSubmission(db, "u2", ac.JobID, ac.URL, ""))
	require.Error(t, unresolvedSubmission(db, "u1", "123", ac.URL, "different-attempt"))
	finishAttempt(db, ac, OutcomeSubmissionUnknown, "confirmation", "unknown", "")
	b := New(Config{DB: db, UserID: "u1"})
	err := b.easyApply(context.Background(), nil, &lazyDocGen{job: linkedInJob{ID: "123", URL: ac.URL}})
	require.ErrorIs(t, err, errLinkedInSubmissionUnknown)
	finishAttempt(db, ac, OutcomeFailed, "entry", "before_click", "")
	require.NoError(t, unresolvedSubmission(db, "u1", "123", ac.URL, ""))
}
