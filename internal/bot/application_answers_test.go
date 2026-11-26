package bot

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/applicationanswers"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/quota"
)

type answerRecorderStub struct {
	proposals          []domain.ApplicationAnswerEncounter
	recordErr, fillErr error
	filled             []string
}

func (s *answerRecorderStub) RecordEncounter(_ context.Context, _ string, e domain.ApplicationAnswerEncounter) (string, error) {
	s.proposals = append(s.proposals, e)
	return "review", s.recordErr
}
func (s *answerRecorderStub) MarkFilled(_ context.Context, _, _ string, answer string) error {
	s.filled = append(s.filled, answer)
	return s.fillErr
}

func TestApplicationAnswerRuntimeDurabilityHistoryAndWarmReuse(t *testing.T) {
	ctx := context.Background()
	database := openMappingDB(t)
	_, err := database.Exec("INSERT INTO users(id,email,password_hash) VALUES('candidate','candidate@fixture.test','fixture')")
	require.NoError(t, err)
	cfg := config.NewStore(database)
	profile := groundedFixtureProfile()
	require.NoError(t, cfg.Set("candidate", "resume_profile", profile))
	svc := applicationanswers.NewService(database, cfg)
	store := &DBMappingStore{DB: database, UserID: "candidate"}
	server := newGroundedCountingServer(t, func(body []byte) string { return groundedFixtureProposals(extractRefsFromPromptBody(body), false) })
	snap := groundedFixtureSnapshot()
	snap.Fields = snap.Fields[:1]
	interp, err := (&StepInterpreter{LLM: NewLLMFormInterpreter(server.newClient(t)), Mapping: store}).InterpretStep(ctx, snap, profile)
	require.NoError(t, err)
	require.False(t, interp.Paused)
	b := New(Config{DB: database, UserID: "candidate", Profile: profile, ApplicationAnswers: svc, AdaptiveMapping: store})
	lazy := &lazyDocGen{b: b, attemptID: newAttemptID(), job: linkedInJob{ID: "fixture-job", Company: "Fixture", Title: "Coordinator"}}
	ac := newAttemptContext("candidate", "linkedin", lazy.job.ID, "https://fixture.invalid/job", "Coordinator", "Fixture", "fixture", "")
	ac.AttemptID = lazy.attemptID
	beginAttempt(database, ac)
	ids, err := b.prepareAnswerReviews(ctx, lazy, snap, interp, profile)
	require.NoError(t, err)
	require.Len(t, ids, 1)
	page, err := svc.ListReviews(ctx, "candidate", "", "active", 20)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, "not_used", page.Items[0].UseStatus)
	require.Empty(t, page.Items[0].AnswerUsed)
	var learned int
	require.NoError(t, database.QueryRow("SELECT COUNT(*) FROM form_field_mappings").Scan(&learned))
	require.Zero(t, learned, "a review proposal alone never grants cache authority")
	filler := &stubFiller{fillOK: true, submitReady: true}
	se := NewStepExecutor(basicBounds(), noGuard())
	se.AfterFieldFill = b.answerFillRecorder(ids)
	result, err := se.ExecuteStep(ctx, snap, interp, filler)
	require.NoError(t, err)
	require.Equal(t, StepOutcomeSubmitted, result.Outcome)
	writeVerifiedFilledStep(ctx, store, snap, interp, result)
	finishAttempt(database, ac, OutcomeSubmittedConfirmed, "apply", "fixture", "")
	page, err = svc.ListReviews(ctx, "candidate", "", "active", 20)
	require.NoError(t, err)
	require.Equal(t, "submitted", page.Items[0].UseStatus)
	require.Equal(t, interp.Fields[0].Value, page.Items[0].AnswerUsed)
	require.NoError(t, svc.MarkRead(ctx, "candidate"))
	page, err = svc.ListReviews(ctx, "candidate", "", "active", 20)
	require.NoError(t, err)
	require.Equal(t, 1, page.UnresolvedCount)
	require.Zero(t, page.UnreadCount)
	warm, err := (&StepInterpreter{Mapping: store}).InterpretStep(ctx, snap, profile)
	require.NoError(t, err)
	require.False(t, warm.LLMUsed)
	require.Equal(t, SourceMapping, warm.Fields[0].Source)
	require.True(t, eligibleAnswerReview(snap, snap.Fields[0], warm.Fields[0], profile), "warm reuse retains later-review policy without another model call")
	require.Equal(t, int32(2), server.requests.Load())
	before := profileDigest(profile)
	require.NoError(t, svc.RecordSources(ctx, "candidate", map[string]domain.ApplicationAnswerSource{"notice_period": {Source: "cv", Evidence: []string{"Immediately available"}}}, applicationanswers.ProfileRevision(profile)))
	require.Equal(t, before, profileDigest(profile))
	warm, err = (&StepInterpreter{Mapping: store}).InterpretStep(ctx, snap, profile)
	require.NoError(t, err)
	require.False(t, warm.LLMUsed)
	require.Equal(t, int32(2), server.requests.Load())
	oldAnswer := page.Items[0].AnswerUsed
	_, err = svc.Confirm(ctx, "candidate", page.Items[0].ID, domain.ApplicationAnswerConfirmation{Answer: "I coordinate projects and scheduling.", Revision: page.Items[0].Revision})
	require.NoError(t, err)
	history, err := svc.GetReview(ctx, "candidate", page.Items[0].ID)
	require.NoError(t, err)
	require.Equal(t, oldAnswer, history.AnswerUsed)
	require.Equal(t, "submitted", history.UseStatus)
}

func TestApplicationAnswerMissingFactDefersOneJobAndContinuesNext(t *testing.T) {
	ctx := context.Background()
	profile := basicProfile()
	profile.ApplicationDefaults.RequiresSponsorship = nil
	recorder := &answerRecorderStub{}
	llm := &stubLLM{}
	b := New(Config{UserID: "candidate", Profile: profile, ApplicationAnswers: recorder, AdaptiveLLM: llm, Platform: domain.PlatformLinkedIn})
	lazy := &lazyDocGen{b: b, job: linkedInJob{ID: "missing-fact-job", Company: "Fixture"}}
	err := b.prepareLegacyAnswers(ctx, lazy, []formField{{Index: 0, Type: "radio", Question: "Do you require sponsorship?", Required: true, Options: []formFieldOption{{Label: "Yes"}, {Label: "No"}}}})
	require.ErrorIs(t, err, errAdaptiveQuestionReview)
	require.False(t, b.IsPaused())
	require.Len(t, recorder.proposals, 1)
	require.Equal(t, "required", recorder.proposals[0].Status)
	require.Empty(t, recorder.proposals[0].ProposedAnswer)
	require.Zero(t, llm.calls)
	next := &lazyDocGen{b: b, job: linkedInJob{ID: "eligible-job", Company: "Fixture"}}
	require.NoError(t, b.prepareLegacyAnswers(ctx, next, []formField{{Index: 0, Type: "text", Question: "First Name", Required: true}}))
	answer, ok := preparedLegacyAnswer(next, "First Name", nil)
	require.True(t, ok)
	require.Equal(t, profile.PersonalInformation.Name, answer)
	require.False(t, b.IsPaused())
	require.Zero(t, llm.calls)
	optional := SnapField{Ref: "f0", Label: "Notice period", Type: SnapTypeText, Required: false}
	profile.ApplicationDefaults.NoticePeriod = ""
	profile.ApplicationDefaults.Availability = ""
	result, err := (&StepInterpreter{}).InterpretStep(ctx, makeSnap(optional), profile)
	require.NoError(t, err)
	require.False(t, result.Paused)
	require.Equal(t, ActionSkip, result.Fields[0].Action)
}

func TestApplicationAnswerRecordOutageCannotReachSubmit(t *testing.T) {
	profile := groundedFixtureProfile()
	snap := groundedFixtureSnapshot()
	snap.Fields = snap.Fields[:1]
	server := newGroundedCountingServer(t, func(body []byte) string { return groundedFixtureProposals(extractRefsFromPromptBody(body), false) })
	interp, err := (&StepInterpreter{LLM: NewLLMFormInterpreter(server.newClient(t))}).InterpretStep(context.Background(), snap, profile)
	require.NoError(t, err)
	outage := errors.New("review store unavailable")
	recorder := &answerRecorderStub{recordErr: outage}
	b := New(Config{UserID: "candidate", Profile: profile, ApplicationAnswers: recorder})
	_, err = b.prepareAnswerReviews(context.Background(), nil, snap, interp, profile)
	require.ErrorIs(t, err, outage)
	recorder.recordErr = nil
	recorder.fillErr = outage
	ids, err := b.prepareAnswerReviews(context.Background(), nil, snap, interp, profile)
	require.NoError(t, err)
	filler := &stubFiller{fillOK: true, submitReady: true}
	se := NewStepExecutor(basicBounds(), noGuard())
	se.AfterFieldFill = b.answerFillRecorder(ids)
	result, err := se.ExecuteStep(context.Background(), snap, interp, filler)
	require.ErrorIs(t, err, outage)
	require.Equal(t, StepOutcomeError, result.Outcome)
	require.Zero(t, filler.navCalls)
	require.Zero(t, filler.submitClicks)
	forged := interp.Fields[0]
	forged.grounded = nil
	require.False(t, eligibleAnswerReview(snap, snap.Fields[0], forged, profile))
}

func TestApplicationAnswerOperationalErrorsNeverBecomeCandidateReview(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded, quota.ErrExceeded, ErrLLMUnavailable, ErrLLMLimitExceeded, errors.New("browser disconnected")} {
		t.Run(cause.Error(), func(t *testing.T) {
			_, err := (&StepInterpreter{LLM: &stubLLM{err: cause}}).InterpretStep(context.Background(), makeSnap(textField("f0", "Describe your motivation")), basicProfile())
			require.ErrorIs(t, err, cause)
			require.False(t, isCandidateDeferral(errors.Join(errAdaptiveQuestionReview, cause)))
		})
	}
}

func TestApplicationAnswerSponsorshipTimingAndScopedEvidence(t *testing.T) {
	no, yes := false, true
	profile := &domain.ResumeProfile{ApplicationDefaults: domain.ApplicationDefaults{RequiresSponsorship: &no, FutureRequiresSponsorship: &yes, WorkRights: []domain.ApplicationWorkRight{{Country: "Australia", RequiresSponsorship: &no, FutureRequiresSponsorship: &no}, {Country: "Germany", RequiresSponsorship: &yes, FutureRequiresSponsorship: &yes}}}}
	for _, tc := range []struct {
		question, location string
		want               bool
	}{{"Do you require sponsorship?", "Australia", false}, {"Will you require sponsorship in future?", "Australia", false}, {"Do you now or in the future require sponsorship?", "Germany", true}, {"Will you require sponsorship?", "Canada", true}} {
		field := SnapField{Label: tc.question, JobLocation: tc.location}
		value, _, ok := currentSponsorship(field, profile)
		require.True(t, ok)
		require.Equal(t, tc.want, *value)
		require.True(t, sponsorshipEvidenceCompatible(field, bindingEvidenceForField(profile, "application_defaults.requires_sponsorship", field)))
	}
	profile.ApplicationDefaults.FutureRequiresSponsorship = nil
	profile.ApplicationDefaults.WorkRights = nil
	_, _, ok := currentSponsorship(SnapField{Label: "Will you require sponsorship in the future?"}, profile)
	require.False(t, ok)
	require.False(t, sponsorshipEvidenceCompatible(SnapField{Label: "Will you require sponsorship in future?"}, []ProfileEvidence{{Path: "/application_defaults/requires_sponsorship", Quote: "false"}}))
	profile.ApplicationDefaults.CustomAnswers = []domain.ApplicationAnswer{{Question: "Relevant experience?", Answer: "I coordinate projects.", Employer: "Fixture", JobID: "job-a"}}
	evidence := []ProfileEvidence{{Path: "/application_defaults/custom_answers/0/answer", Quote: "I coordinate projects."}}
	require.True(t, customAnswerScopeCompatible(profile, StepSnapshot{EmployerKey: "Fixture"}, SnapField{JobID: "job-a"}, evidence))
	require.False(t, customAnswerScopeCompatible(profile, StepSnapshot{EmployerKey: "Other"}, SnapField{JobID: "job-a"}, evidence))
	require.False(t, customAnswerScopeCompatible(profile, StepSnapshot{EmployerKey: "Fixture"}, SnapField{JobID: "job-b"}, evidence))
}

type scopedPlanMapping struct{ entries map[string]FieldResolution }

func (m *scopedPlanMapping) LookupField(sig, _ string) (FieldResolution, bool) {
	r, ok := m.entries[sig]
	return r, ok
}
func (m *scopedPlanMapping) Write(context.Context, StepSnapshot, []FieldResolution) error { return nil }

func TestApplicationAnswerLegacySameLabelKeepsScannedFieldScope(t *testing.T) {
	page := rodTestPage(t, `<html><body><div class="jobs-easy-apply-modal">
 <fieldset><legend>Candidate contact</legend><div data-test-form-element><label for="own-phone">Phone</label><input id="own-phone" type="tel" required></div></fieldset>
 <fieldset><legend>Referee contact</legend><div data-test-form-element><label for="reference-phone">Phone</label><input id="reference-phone" type="tel" required></div></fieldset>
 <button>Continue</button></div></body></html>`)
	fields := evalScanFields(t, page)
	require.Len(t, fields, 2)
	require.NotEqual(t, fields[0].Group, fields[1].Group)
	profile := basicProfile()
	profile.PersonalInformation.Phone = "0412345678"
	profile.ApplicationDefaults.FormAnswers["reference_phone"] = "0498765432"
	snap := captureStepSnapshot(fields, nil, "linkedin", 0, "")
	mapping := &scopedPlanMapping{entries: map[string]FieldResolution{}}
	for _, f := range snap.Fields {
		mapping.entries[f.FieldSignature("linkedin")] = FieldResolution{Action: ActionFill, IntentType: FieldIntentProfile, BindingKey: "application_defaults.form_answers.reference_phone", Provenance: ProvenanceConfirmedByReviewer}
	}
	recorder := &answerRecorderStub{}
	b := New(Config{Profile: profile, UserID: "candidate", Platform: domain.PlatformLinkedIn, ApplicationAnswers: recorder, AdaptiveMapping: mapping})
	lazy := &lazyDocGen{b: b, job: linkedInJob{ID: "fixture", Company: "Fixture"}}
	filled, hasFields, err := b.fillFormStep(context.Background(), page, lazy)
	require.NoError(t, err)
	require.True(t, filled)
	require.True(t, hasFields)
	state, err := page.Eval(`()=>({own:document.getElementById('own-phone').value,reference:document.getElementById('reference-phone').value})`)
	require.NoError(t, err)
	require.Equal(t, "0412345678", state.Value.Get("own").String())
	require.Equal(t, "0498765432", state.Value.Get("reference").String())
	_, ok := preparedLegacyAnswer(lazy, "Phone", nil)
	require.False(t, ok, "question-only fallback refuses two distinct scanned fields")
}

func TestApplicationAnswerReviewGroupsKeepFactAndControlQualifiers(t *testing.T) {
	profile := basicProfile()
	snap := makeSnap()
	snap.EmployerKey = "Fixture"
	first := SnapField{Ref: "f0", Label: "Availability", Type: SnapTypeText, Required: true, JobContext: "job-a"}
	date := first
	date.Ref = "f1"
	date.Label = "Earliest start date"
	a := reviewEncounterIdentity(nil, snap, first, profile, true)
	b := reviewEncounterIdentity(nil, snap, date, profile, true)
	require.NotEmpty(t, a.GroupKey)
	otherStep := snap
	otherStep.StepIndex = 1
	require.NotEqual(t, a.FieldInstanceKey, reviewEncounterIdentity(nil, otherStep, first, profile, true).FieldInstanceKey, "another step is a separate historical control")
	require.NotEqual(t, a.GroupKey, b.GroupKey, "availability prose and start date are distinct facts")
	other := first
	other.JobContext = "job-b"
	same := reviewEncounterIdentity(nil, snap, other, profile, true)
	require.Equal(t, a.GroupKey, same.GroupKey)
	require.Equal(t, a.ContextKey, same.ContextKey)
	for _, change := range []func(*SnapField){func(f *SnapField) { f.InputType = "date" }, func(f *SnapField) { f.Locale = "de-DE" }, func(f *SnapField) { f.Pattern = "[A-Z]+" }, func(f *SnapField) { f.MinValue = "1" }, func(f *SnapField) { f.MaxChars = 20 }} {
		changed := first
		change(&changed)
		next := reviewEncounterIdentity(nil, snap, changed, profile, true)
		require.NotEqual(t, a.ContextKey, next.ContextKey)
	}
}

type profileChangingFiller struct {
	stubFiller
	afterFill func()
}

func (f *profileChangingFiller) Fill(ctx context.Context, field SnapField, r FieldResolution) FieldFillResult {
	result := f.stubFiller.Fill(ctx, field, r)
	f.afterFill()
	return result
}

func TestApplicationAnswerConcurrentProfileChangeCannotReachSubmit(t *testing.T) {
	profile := basicProfile()
	recorder := &answerRecorderStub{}
	b := New(Config{UserID: "candidate", Profile: profile, ApplicationAnswers: recorder})
	lazy := &lazyDocGen{b: b}
	lazy.answerProfileDigest = profileDigest(profile)
	se := NewStepExecutor(basicBounds(), noGuard())
	se.BeforeSideEffect = func(context.Context) error { return b.ensureAnswerFactsCurrent(lazy) }
	filler := &profileChangingFiller{stubFiller: stubFiller{fillOK: true, submitReady: true}, afterFill: func() { profile.PersonalInformation.Name = "Newer confirmed name" }}
	result, err := se.ExecuteStep(context.Background(), snapOneField("f0"), singleFieldInterp("f0", ActionFill), filler)
	require.Error(t, err)
	require.Equal(t, StepOutcomeError, result.Outcome)
	require.Zero(t, filler.submitClicks)
	require.Zero(t, filler.navCalls)
	require.False(t, isCandidateDeferral(err))
}

func TestApplicationAnswerKnownMissingAvoidsUnchangedProviderCalls(t *testing.T) {
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
	e.ID = newAttemptID()
	e.Status = "required"
	e.Reason = "Missing supplied hours"
	_, err = svc.RecordEncounter(ctx, "candidate", e)
	require.NoError(t, err)
	providerErr := errors.New("provider must not be called on known missing fact")
	delegate := &stubLLM{err: providerErr}
	result, err := (&StepInterpreter{LLM: delegate, KnownMissing: b.knownMissingChecker(nil)}).InterpretStep(ctx, snap, profile)
	require.NoError(t, err)
	require.True(t, result.Paused)
	require.Zero(t, delegate.calls)
	changed := *profile
	changed.ApplicationDefaults.WorkingHours = "Weekdays only"
	_, err = (&StepInterpreter{LLM: delegate, KnownMissing: b.knownMissingChecker(nil)}).InterpretStep(ctx, snap, &changed)
	require.ErrorIs(t, err, providerErr)
	require.Equal(t, 1, delegate.calls)
	other := snap
	other.EmployerKey = "Other"
	known, err := b.knownMissingChecker(nil)(ctx, other, snap.Fields[0], profile)
	require.NoError(t, err)
	require.False(t, known)
}
