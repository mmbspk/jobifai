package bot

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llm"
)

type legacyAnswerTailor struct {
	quotaFailTailor
	answer string
	err    error
	calls  atomic.Int32
}

func (s *legacyAnswerTailor) AnswerFormQuestion(context.Context, []byte, string, []string) (string, error) {
	s.calls.Add(1)
	return s.answer, s.err
}

func TestLegacyAnswers_EmptyFailedAndInvalidOptionsNeverGuess(t *testing.T) {
	for _, tc := range []struct {
		name, answer string
		err          error
	}{
		{"empty", "", nil},
		{"whitespace", "  ", nil},
		{"invalid", "Probably yes", nil},
		{"provider failure", "", errors.New("fixture provider failure")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tailor := &legacyAnswerTailor{answer: tc.answer, err: tc.err}
			b := New(Config{Profile: basicProfile(), Tailor: tailor})
			lazy := &lazyDocGen{b: b, job: linkedInJob{ID: "fixture"}}
			answer, err := b.answerFormQuestion(context.Background(), lazy, "Can you work weekends?", []string{"Yes", "No"})
			require.Error(t, err)
			require.Empty(t, answer)
			require.False(t, shouldAdvanceApplyForm(err))
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.ErrorIs(t, err, errAdaptiveQuestionReview)
			}
		})
	}
	answer, err := resolveFormFieldAnswer("", nil, "Yes")
	require.ErrorIs(t, err, errAdaptiveQuestionReview)
	require.Empty(t, answer)
	_, err = validatedLegacyAnswer("Yes", []string{"Yes", " YES "})
	require.ErrorIs(t, err, errAdaptiveQuestionReview, "ambiguous duplicate choices are not interchangeable controls")
	answer, err = validatedLegacyAnswer(" no ", []string{"Yes", "No"})
	require.NoError(t, err)
	require.Equal(t, "No", answer)
}

func TestLegacyAnswers_SponsorshipDoesNotEstablishWorkRightsOrVisaStatus(t *testing.T) {
	for _, question := range []string{"Are you authorized to work in Australia?", "Work authorisation?", "What is your visa status?", "Do you have a work permit?"} {
		for _, sponsor := range []bool{false, true} {
			profile := basicProfile()
			profile.ApplicationDefaults.RequiresSponsorship = &sponsor
			b := New(Config{Profile: profile})
			answer, err := b.answerFormQuestion(context.Background(), nil, question, []string{"Yes", "No"})
			require.ErrorIs(t, err, errAdaptiveQuestionReview, question)
			require.Empty(t, answer, question)
		}
	}
	b := New(Config{Profile: basicProfile()})
	answer, err := b.answerFormQuestion(context.Background(), nil, "Do you require sponsorship?", []string{"Yes", "No"})
	require.NoError(t, err)
	require.Equal(t, "No", answer, "the explicit sponsorship fact still answers its own question")
}

func TestLegacyAnswers_DeclarationCannotBeModelApproval(t *testing.T) {
	tailor := &legacyAnswerTailor{answer: "Yes"}
	b := New(Config{Profile: basicProfile(), Tailor: tailor})
	lazy := &lazyDocGen{b: b, job: linkedInJob{ID: "fixture"}}
	answer, err := b.answerFormQuestion(context.Background(), lazy, "I certify these facts and agree to the terms and conditions", []string{"Yes", "No"})
	require.ErrorIs(t, err, errAdaptiveQuestionReview)
	require.Empty(t, answer)
	require.Zero(t, tailor.calls.Load(), "a model response cannot authorize a declaration")
}

func TestLegacyAnswers_SkillYearsDoNotUseCareerTotal(t *testing.T) {
	profile := basicProfile()
	profile.ExperienceDetails = []domain.ExperienceDetail{{Position: "Sales assistant", EmploymentPeriod: "2010 - 2020"}}
	tailor := &legacyAnswerTailor{answer: "10"}
	b := New(Config{Profile: profile, Tailor: tailor})
	lazy := &lazyDocGen{b: b, job: linkedInJob{ID: "fixture"}}
	answer, err := b.answerFormQuestion(context.Background(), lazy, "How many years of experience do you have with payroll?", nil)
	require.ErrorIs(t, err, errAdaptiveQuestionReview)
	require.Empty(t, answer)
	require.Equal(t, int32(1), tailor.calls.Load(), "there is no career-total heuristic; an unverified duration cannot be used")
}

func TestLegacyAnswers_SensitiveFactsUseIndependentGrounding(t *testing.T) {
	for _, tc := range []struct {
		name, question, answer, path, quote string
		options                             []string
		allowed                             bool
	}{
		{"explicit work right", "Are you authorized to work in Australia?", "Yes", "/application_defaults/work_rights/0/status", "Australian permanent resident", []string{"Yes", "No"}, true},
		{"sponsorship is not work right", "Are you authorized to work in Australia?", "Yes", "/application_defaults/requires_sponsorship", "false", []string{"Yes", "No"}, false},
		{"relevant duration", "How many years of experience do you have with payroll?", "2", "/experience_details/0/employment_period", "2022 - 2024", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			profile := basicProfile()
			profile.ApplicationDefaults.WorkRights = []domain.ApplicationWorkRight{{Country: "Australia", Status: "Australian permanent resident"}}
			profile.ExperienceDetails = []domain.ExperienceDetail{{Position: "Payroll officer", EmploymentPeriod: "2022 - 2024"}, {Position: "Sales assistant", EmploymentPeriod: "2010 - 2020"}}
			proposal := LLMFieldProposal{Ref: "f0", Action: "fill", IntentType: FieldIntentLLM, Value: tc.answer, Evidence: []ProfileEvidence{{Path: tc.path, Quote: tc.quote}}}
			raw, err := json.Marshal([]LLMFieldProposal{proposal})
			require.NoError(t, err)
			server := newGroundedCountingServer(t, func([]byte) string { return string(raw) })
			b := New(Config{Profile: profile, Platform: domain.PlatformLinkedIn, AdaptiveLLM: NewLLMFormInterpreter(server.newClient(t))})
			lazy := &lazyDocGen{b: b, job: linkedInJob{ID: "fixture", Location: "Sydney, Australia", Title: "Payroll officer", Company: "Fixture company"}}
			answer, err := b.answerFormQuestion(context.Background(), lazy, tc.question, tc.options)
			if tc.allowed {
				require.NoError(t, err)
				require.Equal(t, tc.answer, answer)
				require.Equal(t, int32(2), server.requests.Load(), "proposal and independent verification both run")
			} else {
				require.ErrorIs(t, err, errAdaptiveQuestionReview)
				require.Empty(t, answer)
				require.Equal(t, int32(1), server.requests.Load(), "unsupported citation is rejected before verifier approval")
			}
		})
	}
}

func TestLegacyQuestions_UnresolvedWorkRightsStopBothWorkersBeforeNavigation(t *testing.T) {
	for _, platform := range []domain.Platform{domain.PlatformLinkedIn, domain.PlatformSeek} {
		t.Run(string(platform), func(t *testing.T) {
			ctx := context.Background()
			db := openMappingDB(t)
			b := New(Config{DB: db, UserID: "legacy-facts", Platform: platform, Profile: basicProfile()})
			question := `<fieldset><legend>Are you authorized to work in Australia?</legend><input id="work-yes" name="work" type="radio" value="Yes"><label for="work-yes">Yes</label><input id="work-no" name="work" type="radio" value="No"><label for="work-no">No</label></fieldset>`
			br := workerFixtureBrowser(t, func(raw string) string {
				html := linkedInWorkerHTML(linkedInExpectedID(raw), "Acme Inc.")
				if platform == domain.PlatformSeek {
					html = seekWorkerHTML(raw)
				}
				html = strings.ReplaceAll(html, `<label for="notice">How soon can you leave?</label><input id="notice">`, question)
				html = strings.ReplaceAll(html, "next.onclick=()=>{", "next.onclick=()=>{localStorage.setItem('nextClicks','1');")
				return html
			})
			url := "https://www.linkedin.com/jobs/view/92345678"
			if platform == domain.PlatformSeek {
				url = "https://au.seek.com/job/92345678"
			}
			job := linkedInJob{ID: "92345678", URL: url, Title: "Coordinator", Company: "Acme Inc.", Location: "Sydney, Australia"}
			lazy := &lazyDocGen{b: b, ctx: ctx, job: job}
			if platform == domain.PlatformLinkedIn {
				require.False(t, b.submitEasyApply(ctx, br, job, lazy, 8, "fixture", nil, llm.UsageSnapshot{}))
			} else {
				seekJob := seekJob{ID: job.ID, URL: job.URL, Title: job.Title, Company: job.Company, Location: job.Location, EasyApply: true}
				require.NotEqual(t, seekSubmitApplied, b.submitSeekApplication(ctx, br, seekJob, lazy, 8, "fixture", nil, llm.UsageSnapshot{}))
			}
			var applied, blocked int
			require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM jobs_applied WHERE user_id=?`, b.cfg.UserID).Scan(&applied))
			require.Zero(t, applied)
			require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM apply_attempts WHERE user_id=? AND engine='legacy' AND outcome='blocked'`, b.cfg.UserID).Scan(&blocked))
			require.Equal(t, 1, blocked)
			probe := navigateAndWait(t, br, url)
			result, err := probe.Eval(`()=>[localStorage.getItem("nextClicks"),localStorage.getItem("submits")].filter(Boolean).join("|")`)
			require.NoError(t, err)
			require.Empty(t, result.Value.String(), "neither Next nor Submit was dispatched")
		})
	}
}
