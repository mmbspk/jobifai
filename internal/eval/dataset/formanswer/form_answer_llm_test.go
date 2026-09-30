package formanswer_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/eval/dataset"
	"github.com/user/jobifai/internal/eval/dataset/formanswer"
	"github.com/user/jobifai/internal/formfill"
	"github.com/user/jobifai/internal/resume"
)

func TestFormAnswerLLMScenarios_UseForFormFillingAndNotHeuristic(t *testing.T) {
	t.Parallel()
	for _, s := range formanswer.LLMScenarios() {
		t.Run(s.Tag, func(t *testing.T) {
			t.Parallel()
			require.False(t, formfill.QuestionUsesDeterministicHeuristics(s.Question, s.Options),
				"question must reach LLM path")
			got, err := formanswer.FormFillingJSON(s.Profile)
			require.NoError(t, err)
			var round domain.ResumeProfile
			require.NoError(t, json.Unmarshal(got, &round))
			rebuilt, err := formanswer.FormFillingJSON(s.Profile)
			require.NoError(t, err)
			require.JSONEq(t, string(rebuilt), string(got))
			_ = round
		})
	}
}

func TestFormAnswerFullDataset_OnlyLLMBenchmarkCases(t *testing.T) {
	t.Parallel()
	b, err := dataset.Load(dataset.LoadRequest{Task: domain.TaskFormAnswer, Version: "full", Source: dataset.SourceSynthetic})
	require.NoError(t, err)
	for _, c := range b.Cases {
		var in struct {
			ProfileJSON json.RawMessage `json:"profile_json"`
			Question    string          `json:"question"`
			Options     []string        `json:"options"`
		}
		require.NoError(t, json.Unmarshal(c.Input, &in))
		require.False(t, formfill.QuestionUsesDeterministicHeuristics(in.Question, in.Options), "case %s", c.ID)
		var prof domain.ResumeProfile
		require.NoError(t, json.Unmarshal(in.ProfileJSON, &prof))
		expect, err := json.Marshal(resume.ForFormFilling(&prof))
		require.NoError(t, err)
		require.JSONEq(t, string(expect), string(in.ProfileJSON), "case %s profile must be ForFormFilling output", c.ID)
	}
}
