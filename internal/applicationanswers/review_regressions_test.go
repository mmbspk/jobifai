package applicationanswers_test

import (
	"context"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/applicationanswers"
	"github.com/user/jobifai/internal/domain"
	"testing"
)

func TestConfirmTopicsRebasesOnlyUnaffectedReviews(t *testing.T) {
	svc, _, _ := setup(t)
	ctx := context.Background()
	view, err := svc.Get(ctx, "candidate")
	require.NoError(t, err)
	observed := applicationanswers.ProfileRevision(view.Profile)
	ids := map[string]string{}
	for _, topic := range []string{"notice_period", "travel", "affected"} {
		actual := topic
		if topic == "affected" {
			actual = "notice_period"
		}
		id, err := svc.RecordEncounter(ctx, "candidate", domain.ApplicationAnswerEncounter{ID: topic, Question: topic + "?", FieldSignature: topic, TopicID: actual, Status: "suggested", ProposedAnswer: "Old wording", ProfileRevision: observed})
		require.NoError(t, err)
		ids[topic] = id
	}
	first, err := svc.GetReview(ctx, "candidate", ids["notice_period"])
	require.NoError(t, err)
	_, err = svc.Confirm(ctx, "candidate", first.ID, domain.ApplicationAnswerConfirmation{Revision: first.Revision, Answer: "Two weeks", Topics: patch(t, "notice_period", map[string]string{"notice_period": "Two weeks"})})
	require.NoError(t, err)
	first, err = svc.GetReview(ctx, "candidate", first.ID)
	require.NoError(t, err)
	require.Equal(t, "resolved", first.Lifecycle)
	affected, err := svc.GetReview(ctx, "candidate", ids["affected"])
	require.NoError(t, err)
	require.Equal(t, "outdated", affected.Lifecycle)
	_, err = svc.Confirm(ctx, "candidate", affected.ID, domain.ApplicationAnswerConfirmation{Revision: affected.Revision, Answer: "Old wording"})
	require.ErrorIs(t, err, applicationanswers.ErrConflict)
	unrelated, err := svc.GetReview(ctx, "candidate", ids["travel"])
	require.NoError(t, err)
	require.Equal(t, "active", unrelated.Lifecycle)
	require.Equal(t, observed, unrelated.Encounters[0].ProfileRevision)
	_, err = svc.Confirm(ctx, "candidate", unrelated.ID, domain.ApplicationAnswerConfirmation{Revision: unrelated.Revision, ProfileRevision: unrelated.ProfileRevision, Answer: "Occasional travel"})
	require.NoError(t, err)
}
func TestResolvedReviewQuietOnlyForConfirmedAnswerAndFacts(t *testing.T) {
	for _, change := range []string{"unchanged", "proposal", "facts"} {
		t.Run(change, func(t *testing.T) {
			svc, cfg, _ := setup(t)
			ctx := context.Background()
			view, err := svc.Get(ctx, "candidate")
			require.NoError(t, err)
			e := domain.ApplicationAnswerEncounter{ID: "one", Question: "Travel?", FieldSignature: "travel", Status: "suggested", ProposedAnswer: "Occasional", ProfileRevision: applicationanswers.ProfileRevision(view.Profile)}
			id, err := svc.RecordEncounter(ctx, "candidate", e)
			require.NoError(t, err)
			require.NoError(t, svc.MarkFilled(ctx, "candidate", e.ID, "Occasional"))
			review, err := svc.GetReview(ctx, "candidate", id)
			require.NoError(t, err)
			confirmed, err := svc.Confirm(ctx, "candidate", id, domain.ApplicationAnswerConfirmation{Revision: review.Revision, Answer: "Occasional"})
			require.NoError(t, err)
			require.NoError(t, svc.MarkRead(ctx, "candidate"))
			if change == "facts" {
				confirmed.Profile.Summary = "New facts"
				require.NoError(t, cfg.Set("candidate", "resume_profile", confirmed.Profile))
			}
			e.ID = "two"
			e.ProfileRevision = applicationanswers.ProfileRevision(confirmed.Profile)
			if change == "proposal" {
				e.ProposedAnswer = "Frequent"
			}
			_, err = svc.RecordEncounter(ctx, "candidate", e)
			require.NoError(t, err)
			review, err = svc.GetReview(ctx, "candidate", id)
			require.NoError(t, err)
			require.Len(t, review.Encounters, 2)
			require.Equal(t, "Occasional", review.Encounters[1].AnswerUsed)
			page, err := svc.ListReviews(ctx, "candidate", "", "active", 20)
			require.NoError(t, err)
			if change == "unchanged" {
				require.Equal(t, "resolved", review.Lifecycle)
				require.Zero(t, page.UnresolvedCount)
				require.Zero(t, page.UnreadCount)
			} else {
				require.Equal(t, "active", review.Lifecycle)
				require.Equal(t, 1, page.UnresolvedCount)
				require.Equal(t, 1, page.UnreadCount)
			}
		})
	}
}
func TestWorkArrangementRejectsPreferenceFlags(t *testing.T) {
	svc, _, _ := setup(t)
	ctx := context.Background()
	view, err := svc.Get(ctx, "candidate")
	require.NoError(t, err)
	_, err = svc.Patch(ctx, "candidate", domain.ApplicationAnswersPatch{Revision: view.Revision, Topics: map[string]json.RawMessage{"work_arrangement": json.RawMessage(`{"remote":true,"commute_conditions":"Local"}`)}})
	require.ErrorIs(t, err, applicationanswers.ErrInvalidInput)
	after, err := svc.Get(ctx, "candidate")
	require.NoError(t, err)
	require.Equal(t, view.Revision, after.Revision)
}
func TestEncounterIDCollisionCannotAlterAnotherUser(t *testing.T) {
	svc, _, database := setup(t)
	ctx := context.Background()
	e := domain.ApplicationAnswerEncounter{ID: "collision", Question: "Travel?", FieldSignature: "travel", Status: "suggested", ProposedAnswer: "Occasional", ProfileRevision: "observed"}
	_, err := svc.RecordEncounter(ctx, "candidate", e)
	require.NoError(t, err)
	_, err = svc.RecordEncounter(ctx, "other", e)
	require.ErrorIs(t, err, applicationanswers.ErrConflict)
	var count int
	require.NoError(t, database.QueryRow("SELECT COUNT(*) FROM application_answer_reviews WHERE user_id='other'").Scan(&count))
	require.Zero(t, count)
}
