package bot

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
)

func TestTruncateRunes(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "hello", truncateRunes("hello world", 5))
	assert.Equal(t, "x", truncateRunes("x", 0))
	assert.Equal(t, "ab", truncateRunes("ab", 10))
}

func TestSalaryLimits_PreserveRangesAndPayQualifiers(t *testing.T) {
	for _, tc := range []struct{ supplied, expected string }{
		{"88,000", "88000"},
		{"45.50", "45.50"},
		{"150000-180000", "150000-180000"},
		{"AUD 150,000–180,000 per year in base pay, excluding superannuation", "AUD 150,000–180,000 per year in base pay, excluding superannuation"},
		{"USD 45-50 per hour in total compensation, including benefits", "USD 45-50 per hour in total compensation, including benefits"},
		{"Open to negotiation", "Open to negotiation"},
	} {
		require.Equal(t, tc.expected, sanitizeSalaryFieldAnswer(tc.supplied, 100))
	}
	require.Empty(t, sanitizeSalaryFieldAnswer("88000", 4), "a character cap cannot turn 88000 into 8800")
	require.Empty(t, sanitizeSalaryFieldAnswer("USD 88000 per year", 5), "currency and period cannot be discarded to fit")
	_, err := salaryAnswerForControl(SnapField{Type: SnapTypeNumber, MaxChars: 6}, "150000-180000")
	require.ErrorIs(t, err, errAdaptiveQuestionReview, "a range cannot become one concatenated or selected amount")
	_, err = salaryAnswerForControl(SnapField{Type: SnapTypeText, Label: "Salary in USD per month"}, "AUD 88000 per year")
	require.ErrorIs(t, err, errAdaptiveQuestionReview, "retained units expose a currency/period mismatch")
}

func TestSalaryLimits_GroundingReceivesActualQuestionAndControl(t *testing.T) {
	profile := basicProfile()
	profile.ApplicationDefaults.SalaryExpectation = ""
	minimum, maximum, included := 45.0, 50.0, true
	profile.ApplicationDefaults.SalaryRange = &domain.ApplicationSalaryRange{Minimum: &minimum, Maximum: &maximum, Currency: "USD", PayPeriod: "hour", Basis: "total", IncludesBenefits: &included, BenefitsDetails: "benefits"}
	answer := "USD 45-50 per hour in total compensation, including benefits"
	proposal := LLMFieldProposal{Ref: "f0", Action: "fill", IntentType: FieldIntentLLM, Value: answer, Evidence: []ProfileEvidence{
		{Path: "/application_defaults/salary_range/minimum", Quote: "45"},
		{Path: "/application_defaults/salary_range/maximum", Quote: "50"},
		{Path: "/application_defaults/salary_range/currency", Quote: "USD"},
		{Path: "/application_defaults/salary_range/pay_period", Quote: "hour"},
		{Path: "/application_defaults/salary_range/basis", Quote: "total"},
		{Path: "/application_defaults/salary_range/includes_benefits", Quote: "true"},
		{Path: "/application_defaults/salary_range/benefits_details", Quote: "benefits"},
	}}
	raw, err := json.Marshal([]LLMFieldProposal{proposal})
	require.NoError(t, err)
	prompts := make(chan string, 1)
	server := newGroundedCountingServer(t, func(body []byte) string { prompts <- string(body); return string(raw) })
	b := New(Config{Profile: profile, Platform: domain.PlatformLinkedIn, AdaptiveLLM: NewLLMFormInterpreter(server.newClient(t))})
	lazy := &lazyDocGen{b: b, job: linkedInJob{ID: "fixture", Company: "Fixture company"}}
	question := "Salary expectation in USD per hour, total compensation including benefits"
	got, err := b.answerFormQuestionWithLimit(context.Background(), lazy, question, nil, 100)
	require.NoError(t, err)
	require.Equal(t, answer, got)
	require.Equal(t, int32(2), server.requests.Load())
	prompt := <-prompts
	require.Contains(t, prompt, question)
	require.Contains(t, prompt, `\"max_chars\":100`)
	require.NotContains(t, prompt, "numeric base salary only", "the helper cannot rewrite total compensation as base pay")
}

func TestSalaryLimits_StructuredFactsCannotBeBypassedByOldFreeText(t *testing.T) {
	profile := basicProfile()
	profile.ApplicationDefaults.SalaryExpectation = "88000"
	minimum, maximum := 150000.0, 180000.0
	profile.ApplicationDefaults.SalaryRange = &domain.ApplicationSalaryRange{Minimum: &minimum, Maximum: &maximum, Currency: "AUD", PayPeriod: "year", Basis: "base"}
	b := New(Config{Profile: profile})
	answer, err := b.answerSalaryFormField(context.Background(), nil, formField{Type: "text", InputType: "numeric", Question: "Salary expectation"})
	require.ErrorIs(t, err, errAdaptiveQuestionReview, "old free text cannot override structured current facts without grounding")
	require.Empty(t, answer)
}

func TestSalaryLimits_HelpAndGroupQualifiersRequireGrounding(t *testing.T) {
	for _, control := range []formField{
		{Type: "text", Question: "Salary expectation", HelpText: "Enter total compensation, including superannuation"},
		{Type: "text", Question: "Salary", Group: "Your current pay"},
	} {
		b := New(Config{Profile: basicProfile()})
		answer, err := b.answerSalaryFormField(context.Background(), nil, control)
		require.ErrorIs(t, err, errAdaptiveQuestionReview, "a simple label cannot discard the actual question context")
		require.Empty(t, answer)
	}
}

func TestSalaryLimits_LiveControlsNeverReceiveConcatenatedOrTruncatedPay(t *testing.T) {
	for _, tc := range []struct {
		name, control, supplied, script, expected string
		allowed                                   bool
	}{
		{"text preserves range", `type="text" maxlength="100"`, "AUD 150,000-180,000 per year, excluding superannuation", "", "AUD 150,000-180,000 per year, excluding superannuation", true},
		{"numeric rejects range", `type="number" maxlength="6"`, "150000-180000", "", "", false},
		{"cap rejects amount", `type="text" maxlength="4"`, "88000", "", "", false},
		{"numeric preserves amount", `type="number" maxlength="5"`, "88000", "", "88000", true},
		{"numeric accepts grouping only", `type="number" maxlength="5"`, "88,000", "", "88000", true},
		{"site truncation holds", `type="text" maxlength="5"`, "88000", `<script>document.getElementById('salary').oninput=e=>{e.target.value=e.target.value.slice(0,3)}</script>`, "880", false},
		{"native bound holds", `type="number" min="100000"`, "88000", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page := rodTestPage(t, `<html><body><dialog data-testid="dialog" open><div data-sdui-screen="com.linkedin.sdui.flagshipnav.jobs.easyapply.EasyApply"><div data-test-form-element=""><label for="salary">Salary expectation</label><input id="salary" required `+tc.control+`></div></div></dialog>`+tc.script+`</body></html>`)
			profile := basicProfile()
			profile.ApplicationDefaults.SalaryExpectation = tc.supplied
			b := New(Config{Profile: profile, Platform: domain.PlatformLinkedIn})
			lazy := &lazyDocGen{b: b, job: linkedInJob{ID: "fixture"}}
			filled, _, err := b.fillFormStep(context.Background(), page, lazy)
			if tc.allowed {
				require.NoError(t, err)
				require.True(t, filled)
			} else {
				require.ErrorIs(t, err, errAdaptiveQuestionReview)
				require.False(t, shouldAdvanceApplyForm(err))
			}
			result, err := page.Eval(`()=>document.getElementById('salary').value`)
			require.NoError(t, err)
			require.Equal(t, tc.expected, result.Value.String())
		})
	}
}
