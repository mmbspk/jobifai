package bot

import (
	"context"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"strings"
	"testing"
)

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
	ac := newAttemptContext("u1", "linkedin", "123", "https://www.linkedin.com/jobs/view/123", "Role", "Company", "test")
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
	ac := newAttemptContext("u1", "linkedin", j.JobID, j.Link, "Role", "Company", "test")
	beginAttempt(db, ac)
	finishAttempt(db, ac, OutcomeSubmissionUnknown, "confirmation", "unknown", "")
	require.False(t, b.processEvaluationRetry(context.Background(), nil, j))
}

func TestUnresolvedSubmission_BlocksOtherAttemptsAndMatchesURL(t *testing.T) {
	db := testDB(t)
	ac := newAttemptContext("u1", "linkedin", "legacy-id", "https://www.linkedin.com/jobs/view/123", "Role", "Company", "test")
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
