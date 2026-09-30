package resume

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
)

func TestPromptParity_ProviderMessagesMatchDirectBuilders(t *testing.T) {
	t.Parallel()
	prof := domain.ResumeProfile{
		PersonalInformation: domain.PersonalInformation{Name: "Alex", Surname: "Example"},
		ExperienceDetails:   []domain.ExperienceDetail{{Position: "Project Manager", Company: "Harbor Co"}},
	}
	job := "Project manager role coordinating regional programs."
	in, err := json.Marshal(map[string]any{"profile": prof, "job_description": job})
	require.NoError(t, err)
	msgs, err := ProviderMessages(domain.TaskJobScoring, in)
	require.NoError(t, err)
	direct, err := BuildJobScoringPrompt(&prof, job)
	require.NoError(t, err)
	require.Equal(t, direct, msgs[0].Content)

	profile := json.RawMessage(`{"personal_information":{"full_name":"Sam"}}`)
	q := "Are you authorized to work?"
	faIn, _ := json.Marshal(map[string]any{"profile_json": profile, "question": q, "options": []string{"Yes", "No"}})
	faMsgs, err := ProviderMessages(domain.TaskFormAnswer, faIn)
	require.NoError(t, err)
	require.Equal(t, BuildFormAnswerPrompt(profile, q, []string{"Yes", "No"}), faMsgs[0].Content)
}
