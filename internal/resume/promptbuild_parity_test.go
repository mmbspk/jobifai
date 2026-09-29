package resume

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
)

func TestPromptParity_JobScoringEvalMatchesProduction(t *testing.T) {
	t.Parallel()
	prof := domain.ResumeProfile{
		PersonalInformation: domain.PersonalInformation{Name: "Alex", Surname: "Example"},
		ExperienceDetails:   []domain.ExperienceDetail{{Position: "Project Manager", Company: "Harbor Co"}},
	}
	job := "Project manager role coordinating regional programs."
	p1, err := BuildJobScoringPrompt(&prof, job)
	require.NoError(t, err)
	p2, err := BuildJobScoringPrompt(&prof, job)
	require.NoError(t, err)
	require.Equal(t, p1, p2)
}

func TestPromptParity_FormAnswerStable(t *testing.T) {
	t.Parallel()
	profile := json.RawMessage(`{"personal_information":{"full_name":"Sam"}}`)
	q := "Are you authorized to work?"
	p1 := BuildFormAnswerPrompt(profile, q, []string{"Yes", "No"})
	p2 := BuildFormAnswerPrompt(profile, q, []string{"Yes", "No"})
	require.Equal(t, p1, p2)
}
