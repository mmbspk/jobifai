package bot

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/applicationanswers"
	"github.com/user/jobifai/internal/browser"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/domain"
)

func TestApplicationAnswerManagerSetupDetectsSavedFactsChangingDuringFill(t *testing.T) {
	ctx := context.Background()
	database := openMappingDB(t)
	_, err := database.Exec("INSERT INTO users(id,email,password_hash) VALUES('candidate','candidate@fixture.test','fixture')")
	require.NoError(t, err)
	profile := basicProfile()
	cfg := config.NewStore(database)
	require.NoError(t, cfg.Set("candidate", "resume_profile", profile))
	secrets := config.NewSecretsStore(database, "fixture-key")
	sessions := browser.NewSessionStore(database, secrets)
	m := NewManager(ctx, database, cfg, secrets, sessions, nil, nil, nil, nil, "")
	m.SetApplicationAnswers(applicationanswers.NewService(database, cfg))
	b, _, err := m.setupBot("candidate", domain.PlatformSeek, "")
	require.NoError(t, err)
	lazy := &lazyDocGen{b: b, answerProfileDigest: profileDigest(b.currentProfile())}
	se := NewStepExecutor(basicBounds(), noGuard())
	se.BeforeSideEffect = func(context.Context) error { return b.ensureAnswerFactsCurrent(lazy) }
	filler := &profileChangingFiller{stubFiller: stubFiller{fillOK: true, submitReady: true}, afterFill: func() {
		profile.ApplicationDefaults.NoticePeriod = "3 weeks"
		require.NoError(t, cfg.Set("candidate", "resume_profile", profile))
	}}
	result, err := se.ExecuteStep(ctx, snapOneField("f0"), singleFieldInterp("f0", ActionFill), filler)
	require.Error(t, err)
	require.Equal(t, StepOutcomeError, result.Outcome)
	require.Zero(t, filler.navCalls)
	require.Zero(t, filler.submitClicks)
	require.False(t, isCandidateDeferral(err))
}

func TestApplicationAnswerDismissedRepeatsStayQuietUntilFactsChange(t *testing.T) {
	ctx := context.Background()
	database := openMappingDB(t)
	_, err := database.Exec("INSERT INTO users(id,email,password_hash) VALUES('candidate','candidate@fixture.test','fixture')")
	require.NoError(t, err)
	profile := basicProfile()
	cfg := config.NewStore(database)
	require.NoError(t, cfg.Set("candidate", "resume_profile", profile))
	svc := applicationanswers.NewService(database, cfg)
	b := New(Config{UserID: "candidate", Profile: profile, ApplicationAnswers: svc})
	snap := makeSnap(SnapField{Ref: "f0", Label: "What are your preferred weekly hours?", Type: SnapTypeTextarea, Required: true})
	snap.EmployerKey = "Fixture"
	e := reviewEncounterIdentity(nil, snap, snap.Fields[0], profile, true)
	e.ID, e.Status, e.Reason = newAttemptID(), "required", "Missing supplied hours"
	_, err = svc.RecordEncounter(ctx, "candidate", e)
	require.NoError(t, err)
	page, err := svc.ListReviews(ctx, "candidate", "", "active", 20)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	reviewID := page.Items[0].ID
	require.NoError(t, svc.MarkRead(ctx, "candidate"))
	require.NoError(t, svc.Dismiss(ctx, "candidate", reviewID, page.Items[0].Revision))

	delegate := &stubLLM{proposals: []LLMFieldProposal{{Ref: "f0", Action: "skip"}}}
	interpreter := &StepInterpreter{LLM: delegate, KnownMissing: b.knownMissingChecker(nil)}
	for i := 0; i < 2; i++ {
		result, err := interpreter.InterpretStep(ctx, snap, profile)
		require.NoError(t, err)
		require.True(t, result.Paused)
		_, err = b.prepareAnswerReviews(ctx, nil, snap, result, profile)
		require.NoError(t, err)
	}
	require.Zero(t, delegate.calls, "a dismissed unchanged missing fact must not trigger another provider request")
	page, err = svc.ListReviews(ctx, "candidate", "", "active", 20)
	require.NoError(t, err)
	require.Empty(t, page.Items)
	require.Zero(t, page.UnreadCount)
	require.Zero(t, page.UnresolvedCount)
	history, err := svc.GetReview(ctx, "candidate", reviewID)
	require.NoError(t, err)
	require.Equal(t, "dismissed", history.Lifecycle)
	require.Len(t, history.Encounters, 3, "quiet repeats still preserve each observation")

	// Changed profile facts permit a new interpretation. Weekdays alone does
	// not supply the number of weekly hours, so this fixture still defers.
	profile.ApplicationDefaults.WorkingHours = "Weekdays only"
	require.NoError(t, cfg.Set("candidate", "resume_profile", profile))
	result, err := interpreter.InterpretStep(ctx, snap, profile)
	require.NoError(t, err)
	require.True(t, result.Paused)
	require.Equal(t, 1, delegate.calls)
	_, err = b.prepareAnswerReviews(ctx, nil, snap, result, profile)
	require.NoError(t, err)
	page, err = svc.ListReviews(ctx, "candidate", "", "active", 20)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, reviewID, page.Items[0].ID, "changed facts reopen the same scoped decision")
	require.Equal(t, 1, page.UnreadCount)
	require.Equal(t, 1, page.UnresolvedCount)
}

func TestApplicationAnswerWarmReuseRecordsSeparateImmutableAttemptUses(t *testing.T) {
	ctx := context.Background()
	database := openMappingDB(t)
	_, err := database.Exec("INSERT INTO users(id,email,password_hash) VALUES('candidate','candidate@fixture.test','fixture')")
	require.NoError(t, err)
	profile := groundedFixtureProfile()
	cfg := config.NewStore(database)
	require.NoError(t, cfg.Set("candidate", "resume_profile", profile))
	svc := applicationanswers.NewService(database, cfg)
	svc.ConfirmMapping = ConfirmFormMappingTx
	store := &DBMappingStore{DB: database, UserID: "candidate"}
	server := newGroundedCountingServer(t, func(body []byte) string {
		return groundedFixtureProposals(extractRefsFromPromptBody(body), false)
	})
	snap := groundedFixtureSnapshot()
	snap.Fields = snap.Fields[:1]
	b := New(Config{DB: database, UserID: "candidate", Profile: profile, ApplicationAnswers: svc, AdaptiveMapping: store})
	cold, err := (&StepInterpreter{LLM: NewLLMFormInterpreter(server.newClient(t)), Mapping: store}).InterpretStep(ctx, snap, profile)
	require.NoError(t, err)
	require.False(t, cold.Paused)

	fillAttempt := func(interp StepInterpretation, outcome AttemptOutcome) string {
		t.Helper()
		lazy := &lazyDocGen{b: b, attemptID: newAttemptID(), job: linkedInJob{ID: "fixture-job", Company: "Fixture", Title: "Coordinator"}}
		ac := newAttemptContext("candidate", "linkedin", lazy.job.ID, "https://fixture.invalid/job", "Coordinator", "Fixture", "fixture", "")
		ac.AttemptID = lazy.attemptID
		beginAttempt(database, ac)
		ids, err := b.prepareAnswerReviews(ctx, lazy, snap, interp, profile)
		require.NoError(t, err)
		require.Len(t, ids, 1)
		se := NewStepExecutor(basicBounds(), noGuard())
		se.AfterFieldFill = b.answerFillRecorder(ids)
		result, err := se.ExecuteStep(ctx, snap, interp, &stubFiller{fillOK: true, submitReady: true})
		require.NoError(t, err)
		require.Equal(t, StepOutcomeSubmitted, result.Outcome)
		writeVerifiedFilledStep(ctx, store, snap, interp, result)
		finishAttempt(database, ac, outcome, "apply", "fixture", "")
		return lazy.attemptID
	}
	firstAttempt := fillAttempt(cold, OutcomeSubmittedConfirmed)
	require.Equal(t, int32(2), server.requests.Load(), "one proposal and one independent verification request")
	page, err := svc.ListReviews(ctx, "candidate", "", "active", 20)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	reviewID := page.Items[0].ID
	require.NoError(t, svc.MarkRead(ctx, "candidate"))

	warm, err := (&StepInterpreter{LLM: NewLLMFormInterpreter(server.newClient(t)), Mapping: store}).InterpretStep(ctx, snap, profile)
	require.NoError(t, err)
	require.False(t, warm.Paused)
	require.False(t, warm.LLMUsed)
	require.Equal(t, SourceMapping, warm.Fields[0].Source)
	secondAttempt := fillAttempt(warm, OutcomeSubmissionUnknown)
	require.NotEqual(t, firstAttempt, secondAttempt)
	require.Equal(t, int32(2), server.requests.Load(), "warm fill and recording add no provider requests")
	page, err = svc.ListReviews(ctx, "candidate", "", "active", 20)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Zero(t, page.UnreadCount, "the same unresolved decision does not send another notification")
	history, err := svc.GetReview(ctx, "candidate", reviewID)
	require.NoError(t, err)
	require.Len(t, history.Encounters, 2)
	require.Equal(t, secondAttempt, history.Encounters[0].AttemptID)
	require.Equal(t, "submission_unknown", history.Encounters[0].UseStatus)
	require.Equal(t, firstAttempt, history.Encounters[1].AttemptID)
	require.Equal(t, "submitted", history.Encounters[1].UseStatus)
	require.NotEqual(t, history.Encounters[0].ID, history.Encounters[1].ID)
	for _, e := range history.Encounters {
		require.Equal(t, cold.Fields[0].Value, e.AnswerUsed)
	}
	_, err = svc.Confirm(ctx, "candidate", reviewID, domain.ApplicationAnswerConfirmation{Answer: "I coordinate projects and scheduling.", Revision: page.Items[0].Revision})
	require.NoError(t, err)
	after, err := svc.GetReview(ctx, "candidate", reviewID)
	require.NoError(t, err)
	require.Equal(t, history.Encounters, after.Encounters, "a future correction cannot relabel or rewrite either past attempt")
}

func TestApplicationAnswerConfirmedDeferralAllowsOnlySafeFutureRun(t *testing.T) {
	for _, tc := range []struct {
		name, failureCode string
		outcome           AttemptOutcome
		queue             string
		wantReason        string
	}{
		{name: "corrected blocked fact", failureCode: "answer_confirmed", outcome: OutcomeBlocked},
		{name: "unconfirmed fact", failureCode: "answer_confirmation", outcome: OutcomeBlocked, wantReason: "in skipped list"},
		{name: "unknown submission", failureCode: "answer_confirmed", outcome: OutcomeSubmissionUnknown, wantReason: "in skipped list"},
		{name: "active attempt", failureCode: "answer_confirmed", outcome: OutcomeInProgress, wantReason: "in skipped list"},
		{name: "confirmed submission", failureCode: "answer_confirmed", outcome: OutcomeSubmittedConfirmed, wantReason: "in skipped list"},
		{name: "still in review queue", failureCode: "answer_confirmed", outcome: OutcomeBlocked, queue: "jobs_pending_review", wantReason: "in Top Matches"},
		{name: "still approved", failureCode: "answer_confirmed", outcome: OutcomeBlocked, queue: "jobs_approved_queue", wantReason: "in approved queue"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database := openMappingDB(t)
			_, err := database.Exec(`INSERT INTO jobs_skipped(id,user_id,platform,company,role,link,skip_reason,failure_code,viewed_at) VALUES('job','candidate','seek','Fixture','Role','https://fixture.invalid/job','missing supplied fact',?,datetime('now'))`, tc.failureCode)
			require.NoError(t, err)
			ac := newAttemptContext("candidate", "seek", "job", "https://fixture.invalid/job", "Role", "Fixture", "fixture", "")
			beginAttempt(database, ac)
			if tc.outcome != OutcomeInProgress {
				finishAttempt(database, ac, tc.outcome, "apply", "fixture", "")
			}
			if tc.queue != "" {
				_, err = database.Exec(`INSERT INTO ` + tc.queue + `(job_id,user_id,platform,company,role,link) VALUES('job','candidate','seek','Fixture','Role','https://fixture.invalid/job')`)
				require.NoError(t, err)
			}
			cache := newJobSeenCache()
			_, err = cache.load(database, "candidate")
			require.NoError(t, err)
			require.Equal(t, tc.wantReason, cache.reason("job"))
			err = unresolvedSubmission(database, "candidate", "job", "https://fixture.invalid/job", "new-attempt")
			if tc.outcome == OutcomeInProgress || tc.outcome == OutcomeSubmissionUnknown {
				require.Error(t, err, "direct retry also retains unresolved submission holds")
			} else {
				require.NoError(t, err)
			}
		})
	}
}
