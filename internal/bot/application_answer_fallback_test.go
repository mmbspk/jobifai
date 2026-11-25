package bot

import (
	"context"
	"strings"
	"testing"

	"github.com/go-rod/rod"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
)

func TestApplicationAnswerPrepareUsesSharedPlanForFactsAndMissingAnswers(t *testing.T) {
	profile := basicProfile()
	profile.ApplicationDefaults.RequiresSponsorship = nil
	recorder := &answerRecorderStub{}
	tailor := &legacyAnswerTailor{answer: "Yes"}
	llm := &stubLLM{}
	b := New(Config{UserID: "candidate", Profile: profile, ApplicationAnswers: recorder, Tailor: tailor, AdaptiveLLM: llm})
	lazy := &lazyDocGen{b: b, reviewPrepareOnly: true, job: linkedInJob{ID: "prepare-job"}}
	require.NoError(t, b.prepareLegacyAnswers(context.Background(), lazy, []formField{{Type: "text", Question: "First Name", Required: true}}))
	answer, ok := preparedLegacyAnswer(lazy, "First Name", nil)
	require.True(t, ok)
	require.Equal(t, profile.PersonalInformation.Name, answer)
	err := b.prepareLegacyAnswers(context.Background(), lazy, []formField{{Type: "radio", Question: "Do you require sponsorship?", Required: true, Options: []formFieldOption{{Label: "Yes"}, {Label: "No"}}}})
	require.ErrorIs(t, err, errAdaptiveQuestionReview)
	require.Len(t, recorder.proposals, 1)
	require.Equal(t, "required", recorder.proposals[0].Status)
	require.Empty(t, recorder.proposals[0].ProposedAnswer)
	require.Zero(t, llm.calls)
	require.Zero(t, tailor.calls.Load())
	require.False(t, b.IsPaused())
}

func TestApplicationAnswerPrepareRejectsUnverifiedCompositionWithoutLearning(t *testing.T) {
	ctx := context.Background()
	profile := groundedFixtureProfile()
	database := openMappingDB(t)
	store := &DBMappingStore{DB: database, UserID: "candidate"}
	recorder := &answerRecorderStub{}
	server := newCountingServer(t, func(body []byte) string {
		if strings.Contains(string(body), "VERIFY_GROUNDED_FORM_ANSWERS_V1") {
			return fixtureGroundedVerdicts(body, false)
		}
		return groundedFixtureProposals(extractRefsFromPromptBody(body), false)
	})
	tailor := &legacyAnswerTailor{answer: "I will work any hours and travel wherever required."}
	b := New(Config{UserID: "candidate", Profile: profile, ApplicationAnswers: recorder, AdaptiveMapping: store, AdaptiveLLM: NewLLMFormInterpreter(server.newClient(t)), Tailor: tailor})
	lazy := &lazyDocGen{b: b, reviewPrepareOnly: true, job: linkedInJob{ID: "prepare-job", Company: "Fixture"}}
	err := b.prepareLegacyAnswers(ctx, lazy, []formField{{Type: "text", InputType: "textarea", Question: "Describe your relevant experience for this role", Required: true}})
	require.ErrorIs(t, err, errAdaptiveQuestionReview)
	require.Nil(t, lazy.answerPlan)
	require.Len(t, recorder.proposals, 1)
	require.Empty(t, recorder.proposals[0].ProposedAnswer)
	require.Empty(t, recorder.filled)
	var learned int
	require.NoError(t, database.QueryRow("SELECT COUNT(*) FROM form_field_mappings WHERE provenance != 'pending'").Scan(&learned))
	require.Zero(t, learned)
	require.Zero(t, tailor.calls.Load())
	require.EqualValues(t, 2, server.requests.Load())
}

func TestApplicationAnswerUnplannedControlCannotCallLegacyProvider(t *testing.T) {
	tailor := &legacyAnswerTailor{answer: "Yes"}
	b := New(Config{Profile: basicProfile(), Tailor: tailor, ApplicationAnswers: &answerRecorderStub{}})
	lazy := &lazyDocGen{b: b, reviewPrepareOnly: true, job: linkedInJob{ID: "prepare-job"}}
	answer, err := b.answerFormQuestion(context.Background(), lazy, "Are you willing to travel overseas weekly?", []string{"Yes", "No"})
	require.ErrorIs(t, err, errUnresolvedLegacyControl)
	require.False(t, isCandidateDeferral(err))
	require.Empty(t, answer)
	require.Zero(t, tailor.calls.Load())
}

func TestApplicationAnswerVisionRecoveryRequiresCurrentDOMAndRecordsVerifiedUse(t *testing.T) {
	br := launchTestBrowser(t)
	const jobID = "fallback-job"
	srv := fixtureServer(t, jobID, "", `<html><body><div class="jobs-easy-apply-modal"><div data-test-form-element><label for="experience">Describe your relevant experience for this role</label><textarea id="experience" required></textarea></div><button>Continue</button></div></body></html>`, "")
	page := navigateAndWait(t, br, srv.URL+"/jobs/view/"+jobID)
	database := openMappingDB(t)
	recorder := &answerRecorderStub{}
	server := newGroundedCountingServer(t, func(body []byte) string { return groundedFixtureProposals(extractRefsFromPromptBody(body), false) })
	tailor := &legacyAnswerTailor{answer: "unsupported legacy assertion"}
	b := New(Config{UserID: "candidate", Profile: groundedFixtureProfile(), Platform: domain.PlatformLinkedIn, Tailor: tailor, ApplicationAnswers: recorder, AdaptiveLLM: NewLLMFormInterpreter(server.newClient(t)), AdaptiveMapping: &DBMappingStore{DB: database, UserID: "candidate"}})
	lazy := &lazyDocGen{b: b, reviewPrepareOnly: true, job: linkedInJob{ID: jobID, Company: "Fixture", URL: srv.URL + "/jobs/view/" + jobID}}
	filled, err := b.fillFormStepFromVision(context.Background(), page, lazy)
	require.NoError(t, err)
	require.True(t, filled)
	require.Len(t, recorder.proposals, 1)
	require.Equal(t, "suggested", recorder.proposals[0].Status)
	require.Equal(t, []string{"I coordinate client projects and improve scheduling processes."}, recorder.filled)
	value, err := page.Eval(`()=>document.getElementById('experience').value`)
	require.NoError(t, err)
	require.Equal(t, recorder.filled[0], value.Value.String())
	require.Zero(t, tailor.calls.Load())
	var learned int
	require.NoError(t, database.QueryRow("SELECT COUNT(*) FROM form_field_mappings WHERE provenance != 'pending'").Scan(&learned))
	require.Equal(t, 1, learned)
}

func TestApplicationAnswerVisionUnresolvedContainerDoesNotUsePredictedAnswer(t *testing.T) {
	br := launchTestBrowser(t)
	page := br.MustPage("")
	require.NoError(t, page.SetDocumentContent(`<html><body><label for="travel">Can you travel?</label><input id="travel"></body></html>`))
	tailor := &legacyAnswerTailor{answer: "Yes"}
	recorder := &answerRecorderStub{}
	b := New(Config{Profile: basicProfile(), Tailor: tailor, ApplicationAnswers: recorder})
	lazy := &lazyDocGen{b: b, job: linkedInJob{ID: "unresolved-job"}}
	filled, err := b.fillFormStepFromVision(context.Background(), page, lazy)
	require.ErrorIs(t, err, errUnresolvedLegacyControl)
	require.False(t, filled)
	require.False(t, isCandidateDeferral(err))
	require.Empty(t, recorder.proposals)
	require.Empty(t, recorder.filled)
	require.Zero(t, tailor.calls.Load())
	value, err := page.Eval(`()=>document.getElementById('travel').value`)
	require.NoError(t, err)
	require.Empty(t, value.Value.String())
}

func TestApplicationAnswerSearchPreferencesCannotProveWillingness(t *testing.T) {
	tailor := &legacyAnswerTailor{answer: "Yes"}
	recorder := &answerRecorderStub{}
	llm := &stubLLM{proposals: []LLMFieldProposal{{Ref: "f0", Action: "fill", IntentType: FieldIntentLLM, Value: "Yes"}}}
	b := New(Config{UserID: "candidate", Profile: basicProfile(), ApplicationAnswers: recorder, Tailor: tailor, AdaptiveLLM: llm, Preferences: domain.WorkPreferences{Locations: []string{"Hobart"}}})
	lazy := &lazyDocGen{b: b, reviewPrepareOnly: true, job: linkedInJob{ID: "preferences-job", Location: "Hobart"}}
	err := b.prepareLegacyAnswers(context.Background(), lazy, []formField{{Type: "radio", Question: "Are you willing to commute to this role in Hobart every weekday?", Required: true, Options: []formFieldOption{{Label: "Yes"}, {Label: "No"}}}})
	require.ErrorIs(t, err, errAdaptiveQuestionReview)
	require.Len(t, recorder.proposals, 1)
	require.Empty(t, recorder.proposals[0].ProposedAnswer)
	require.Empty(t, recorder.filled)
	require.Zero(t, tailor.calls.Load())
	require.NotContains(t, string(lazy.formProfileJSON()), "Hobart")
}

func TestApplicationAnswerSeekPrepareStopsBeforeNavigationOnMissingFact(t *testing.T) {
	br := launchTestBrowser(t)
	page := br.MustPage("")
	// Every request is answered locally, including the deliberately realistic
	// origin needed by SEEK's apply-link ownership checks. No request reaches SEEK.
	router := page.HijackRequests()
	router.MustAdd("*", func(h *rod.Hijack) {
		h.Response.SetHeader("Content-Type", "text/html; charset=utf-8")
		h.Response.SetBody(`<html><body><a data-automation="job-detail-apply" href="/apply/12345678">Apply</a>
<form data-testid="quick-apply-form"><label for="notice">Notice period</label><input id="notice" required>
<button type="button" data-testid="continue-button" onclick="window._continueClicks=(window._continueClicks||0)+1">Continue</button></form></body></html>`)
	})
	go router.Run()
	t.Cleanup(func() { router.MustStop() })
	const jobURL = "https://www.seek.com.au/job/12345678"
	require.NoError(t, page.Navigate(jobURL))
	require.NoError(t, page.WaitLoad())
	profile := basicProfile()
	profile.ApplicationDefaults.NoticePeriod = ""
	profile.ApplicationDefaults.Availability = ""
	recorder := &answerRecorderStub{}
	b := New(Config{UserID: "candidate", Profile: profile, ApplicationAnswers: recorder, Platform: domain.PlatformSeek})
	lazy := &lazyDocGen{b: b, reviewPrepareOnly: true, job: linkedInJob{ID: "12345678", URL: jobURL}}
	err := (&Manager{}).prepareSeekReviewDocuments(context.Background(), b, page, lazy, jobURL)
	require.ErrorIs(t, err, errAdaptiveQuestionReview)
	require.False(t, lazy.prepareScanComplete)
	require.Len(t, recorder.proposals, 1)
	clicks, err := page.Eval(`()=>window._continueClicks||0`)
	require.NoError(t, err)
	require.Zero(t, clicks.Value.Int())
}
