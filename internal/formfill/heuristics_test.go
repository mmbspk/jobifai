package formfill_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/formfill"
)

func TestQuestionUsesDeterministicHeuristics_SponsorshipAndAuth(t *testing.T) {
	t.Parallel()
	require.True(t, formfill.QuestionUsesDeterministicHeuristics("Do you require sponsorship?", []string{"Yes", "No"}))
	require.True(t, formfill.QuestionUsesDeterministicHeuristics("Are you authorized to work in Australia?", []string{"Yes", "No"}))
}

func TestQuestionUsesDeterministicHeuristics_ContactAndYears(t *testing.T) {
	t.Parallel()
	require.True(t, formfill.QuestionUsesDeterministicHeuristics("Your email address?", nil))
	require.True(t, formfill.QuestionUsesDeterministicHeuristics("How many years of experience do you have?", nil))
}

func TestQuestionUsesDeterministicHeuristics_RoleSkillUsesLLM(t *testing.T) {
	t.Parallel()
	require.False(t, formfill.QuestionUsesDeterministicHeuristics("Do you have experience with Epic EHR?", []string{"Yes", "No"}))
	require.False(t, formfill.QuestionUsesDeterministicHeuristics("What is your expected salary (AUD)?", nil))
}
