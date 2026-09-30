package validators

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSponsorshipClassification_MutuallyExclusive(t *testing.T) {
	t.Parallel()
	yesExamples := []string{
		"Yes",
		"Yes, I require employer sponsorship.",
		"I require employer sponsorship.",
		"I will require visa sponsorship.",
		"I would need sponsorship.",
	}
	noExamples := []string{
		"No",
		"No, I do not require sponsorship.",
		"I do not require employer sponsorship.",
		"I don't require sponsorship.",
		"No sponsorship is required.",
	}
	for _, s := range yesExamples {
		require.True(t, matchBooleanYes(s), s)
		require.False(t, matchBooleanNo(s), s)
	}
	for _, s := range noExamples {
		require.True(t, matchBooleanNo(s), s)
		require.False(t, matchBooleanYes(s), s)
	}
	require.False(t, matchBooleanYes("I do not require employer sponsorship."))
	require.True(t, matchBooleanNo("I do not require employer sponsorship."))
	all := append(append([]string{}, yesExamples...), noExamples...)
	for _, s := range all {
		require.False(t, matchBooleanYes(s) && matchBooleanNo(s), "both matched: %q", s)
	}
}

func TestMatchDuration_BoundarySafe(t *testing.T) {
	t.Parallel()
	want := "8 weeks"
	for _, pass := range []string{"8 weeks", "My notice period is 8 weeks.", "I need to provide eight weeks notice"} {
		require.True(t, matchDuration(pass, want), pass)
	}
	for _, fail := range []string{"18 weeks", "8 months", "4 weeks"} {
		require.False(t, matchDuration(fail, want), fail)
	}
}

func TestMatchCurrencyAmount_NoSubstringFalsePositive(t *testing.T) {
	t.Parallel()
	want := "88000 AUD"
	for _, pass := range []string{"88000", "88,000", "$88,000", "AUD 88,000", "My salary expectation is AUD 88,000."} {
		require.True(t, matchCurrencyAmount(pass, want), pass)
	}
	for _, fail := range []string{"188000", "8800", "98000"} {
		require.False(t, matchCurrencyAmount(fail, want), fail)
	}
}
