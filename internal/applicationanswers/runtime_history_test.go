package applicationanswers_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/applicationanswers"
	"github.com/user/jobifai/internal/domain"
)

func TestSponsorshipObjectAndLegacyPayloadKeepCurrentFutureDistinct(t *testing.T) {
	svc, cfg, _ := setup(t)
	ctx := context.Background()
	no := false
	p := domain.ResumeProfile{ApplicationDefaults: domain.ApplicationDefaults{RequiresSponsorship: &no, FutureRequiresSponsorship: &no, FormAnswers: map[string]string{"mapping_existing": "Preserve this"}}}
	require.NoError(t, cfg.Set("candidate", "resume_profile", p))
	view, err := svc.Get(ctx, "candidate")
	require.NoError(t, err)
	view, err = svc.Patch(ctx, "candidate", domain.ApplicationAnswersPatch{Revision: view.Revision, Topics: patch(t, "sponsorship", map[string]any{"requires_sponsorship": false, "future_requires_sponsorship": true})})
	require.NoError(t, err)
	require.False(t, *view.ApplicationDefaults.RequiresSponsorship)
	require.True(t, *view.ApplicationDefaults.FutureRequiresSponsorship)
	view, err = svc.Patch(ctx, "candidate", domain.ApplicationAnswersPatch{Revision: view.Revision, Topics: patch(t, "sponsorship", nil)})
	require.NoError(t, err)
	require.Nil(t, view.ApplicationDefaults.RequiresSponsorship)
	require.True(t, *view.ApplicationDefaults.FutureRequiresSponsorship)
	view, err = svc.Patch(ctx, "candidate", domain.ApplicationAnswersPatch{Revision: view.Revision, Topics: patch(t, "sponsorship", map[string]any{"future_requires_sponsorship": false})})
	require.NoError(t, err)
	require.Nil(t, view.ApplicationDefaults.RequiresSponsorship)
	require.False(t, *view.ApplicationDefaults.FutureRequiresSponsorship)
	require.Equal(t, "Preserve this", view.ApplicationDefaults.FormAnswers["mapping_existing"])
}

func TestNullTopicSaveClearsSuppliedDatesAndTextWithoutChangingOtherFacts(t *testing.T) {
	svc, cfg, _ := setup(t)
	ctx := context.Background()
	p := domain.ResumeProfile{ApplicationDefaults: domain.ApplicationDefaults{EarliestStartDate: "2027-01-18", TravelConditions: "Weekday travel", WorkingHours: "Full time", NoticePeriod: "Two weeks", FormAnswers: map[string]string{"legacy": "Keep this"}}}
	require.NoError(t, cfg.Set("candidate", "resume_profile", p))
	view, err := svc.Get(ctx, "candidate")
	require.NoError(t, err)
	topics := patch(t, "earliest_start_date", nil)
	for key, raw := range patch(t, "travel", nil) {
		topics[key] = raw
	}
	for key, raw := range patch(t, "working_hours", nil) {
		topics[key] = raw
	}
	view, err = svc.Patch(ctx, "candidate", domain.ApplicationAnswersPatch{Revision: view.Revision, Topics: topics})
	require.NoError(t, err)
	require.Empty(t, view.ApplicationDefaults.EarliestStartDate)
	require.Empty(t, view.ApplicationDefaults.TravelConditions)
	require.Empty(t, view.ApplicationDefaults.WorkingHours)
	require.Equal(t, "Two weeks", view.ApplicationDefaults.NoticePeriod)
	require.Equal(t, p.ApplicationDefaults.FormAnswers, view.ApplicationDefaults.FormAnswers)
}

func TestChangedFieldUseKeepsReplacedTextOutOfSubmittedHistory(t *testing.T) {
	svc, cfg, database := setup(t)
	ctx := context.Background()
	p := domain.ResumeProfile{}
	require.NoError(t, cfg.Set("candidate", "resume_profile", p))
	_, err := database.Exec(`INSERT INTO apply_attempts(id,user_id,platform,job_id,outcome,stage) VALUES('attempt','candidate','linkedin','job','in_progress','apply')`)
	require.NoError(t, err)
	e := domain.ApplicationAnswerEncounter{ID: "wide", FieldSignature: "wide-control-shape", FieldInstanceKey: "same-form-control", Question: "Describe your experience", Status: "suggested", ProposedAnswer: "Projects and scheduling", AttemptID: "attempt", JobID: "job", ProfileRevision: applicationanswers.ProfileRevision(&p)}
	oldID, err := svc.RecordEncounter(ctx, "candidate", e)
	require.NoError(t, err)
	require.NoError(t, svc.MarkFilled(ctx, "candidate", e.ID, e.ProposedAnswer))
	e.ID = "narrow"
	e.FieldSignature = "narrow-control-shape"
	e.ProposedAnswer = "Projects"
	newID, err := svc.RecordEncounter(ctx, "candidate", e)
	require.NoError(t, err)
	require.NoError(t, svc.MarkFilled(ctx, "candidate", e.ID, e.ProposedAnswer))
	_, err = database.Exec(`UPDATE apply_attempts SET outcome='submitted_confirmed' WHERE id='attempt'`)
	require.NoError(t, err)
	old, err := svc.GetReview(ctx, "candidate", oldID)
	require.NoError(t, err)
	require.Equal(t, "Projects and scheduling", old.AnswerUsed)
	require.Equal(t, "replaced", old.UseStatus)
	require.Equal(t, "superseded", old.Lifecycle)
	latest, err := svc.GetReview(ctx, "candidate", newID)
	require.NoError(t, err)
	require.Equal(t, "Projects", latest.AnswerUsed)
	require.Equal(t, "submitted", latest.UseStatus)
	require.ErrorIs(t, svc.MarkFilled(ctx, "candidate", "wide", "New text"), applicationanswers.ErrConflict)
	require.ErrorIs(t, svc.MarkFilled(ctx, "other", "narrow", "Projects"), domain.ErrNotFound)
}

func TestEncounterReplayCannotChangeControlOrJobIdentity(t *testing.T) {
	svc, cfg, _ := setup(t)
	ctx := context.Background()
	p := domain.ResumeProfile{}
	require.NoError(t, cfg.Set("candidate", "resume_profile", p))
	e := domain.ApplicationAnswerEncounter{ID: "stable-event", FieldSignature: "same-shape", FieldInstanceKey: "original-control", Question: "Describe your experience", Status: "suggested", ProposedAnswer: "I coordinate projects.", JobID: "job-a", JobTitle: "Coordinator", ProfileRevision: applicationanswers.ProfileRevision(&p)}
	id, err := svc.RecordEncounter(ctx, "candidate", e)
	require.NoError(t, err)
	repeated, err := svc.RecordEncounter(ctx, "candidate", e)
	require.NoError(t, err)
	require.Equal(t, id, repeated)
	for _, change := range []func(*domain.ApplicationAnswerEncounter){
		func(copy *domain.ApplicationAnswerEncounter) { copy.FieldInstanceKey = "another-control" },
		func(copy *domain.ApplicationAnswerEncounter) { copy.JobID = "job-b" },
		func(copy *domain.ApplicationAnswerEncounter) { copy.JobTitle = "Different role" },
	} {
		altered := e
		change(&altered)
		_, err := svc.RecordEncounter(ctx, "candidate", altered)
		require.ErrorIs(t, err, applicationanswers.ErrConflict)
	}
}

func TestAnswerConfirmationReleasesOnlyKnownSafeFactDeferrals(t *testing.T) {
	svc, cfg, database := setup(t)
	ctx := context.Background()
	p := domain.ResumeProfile{}
	require.NoError(t, cfg.Set("candidate", "resume_profile", p))
	var reviewID string
	cases := []struct{ job, outcome, reason, stage string }{{"retry", "blocked", "answer_confirmation", "apply"}, {"unknown", "submission_unknown", "answer_confirmation", "submit_dispatch"}, {"submitted", "submitted_confirmed", "answer_confirmation", "submitted"}, {"technical", "blocked", "identity_unverified", "apply"}}
	for i, tc := range cases {
		attempt := fmt.Sprintf("attempt-%d", i)
		_, err := database.Exec(`INSERT INTO jobs_skipped(id,user_id,platform,company,role,link,skip_reason,failure_code) VALUES(?,'candidate','linkedin','Fixture','Coordinator','https://fixture.invalid/job','easy apply: supplied answer needed','answer_confirmation')`, tc.job)
		require.NoError(t, err)
		_, err = database.Exec(`INSERT INTO apply_attempts(id,user_id,platform,job_id,outcome,reason_code,stage) VALUES(?,'candidate','linkedin',?,?,?,?)`, attempt, tc.job, tc.outcome, tc.reason, tc.stage)
		require.NoError(t, err)
		e := domain.ApplicationAnswerEncounter{ID: fmt.Sprintf("event-%d", i), GroupKey: "same-notice-fact", FieldSignature: "notice", Question: "Notice period", TopicID: "notice_period", Status: "required", AttemptID: attempt, JobID: tc.job, ProfileRevision: applicationanswers.ProfileRevision(&p)}
		reviewID, err = svc.RecordEncounter(ctx, "candidate", e)
		require.NoError(t, err)
	}
	review, err := svc.GetReview(ctx, "candidate", reviewID)
	require.NoError(t, err)
	_, err = svc.Confirm(ctx, "candidate", reviewID, domain.ApplicationAnswerConfirmation{Answer: "Two weeks", Revision: review.Revision})
	require.NoError(t, err)
	for _, tc := range cases {
		var code string
		require.NoError(t, database.QueryRow(`SELECT failure_code FROM jobs_skipped WHERE id=? AND user_id='candidate'`, tc.job).Scan(&code))
		expected := "answer_confirmation"
		if tc.job == "retry" {
			expected = "answer_confirmed"
		}
		require.Equal(t, expected, code, tc.job)
	}
	var attempts int
	require.NoError(t, database.QueryRow(`SELECT COUNT(*) FROM apply_attempts WHERE user_id='candidate'`).Scan(&attempts))
	require.Equal(t, 4, attempts, "confirmation schedules no application")
}
