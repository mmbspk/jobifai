package dataset

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
)

func TestSyntheticFullDatasets_NoPlaceholders(t *testing.T) {
	t.Parallel()
	tasks := []string{
		domain.TaskJobScoring, domain.TaskEmploymentEthics, domain.TaskFormAnswer, domain.TaskFormVision,
		domain.TaskResumeExtract, domain.TaskResumeTailoring, domain.TaskCoverLetter, domain.TaskApplicationQuestions,
	}
	for _, task := range tasks {
		t.Run(task, func(t *testing.T) {
			t.Parallel()
			b, err := Load(LoadRequest{Task: task, Version: "full", Source: SourceSynthetic})
			require.NoError(t, err)
			require.NotEmpty(t, b.Cases)
			for _, c := range b.Cases {
				require.NoError(t, ValidateCaseContent(c))
			}
			r := BuildQualityReport(b)
			require.Greater(t, r.UniqueInputs, 0)
			switch task {
			case domain.TaskJobScoring:
				require.NoError(t, AssertMinimumDiversity(b, 10))
				require.Greater(t, r.ExpectPassCount, 0)
				require.Greater(t, r.ExpectSkipCount, 0)
				require.Less(t, len(r.ScenarioTags), r.TotalCases, "full sets repeat templates; see eval/datasets/BENCHMARK.md")
			case domain.TaskEmploymentEthics:
				require.NoError(t, AssertMinimumDiversity(b, 15))
			case domain.TaskFormAnswer:
				require.NoError(t, AssertMinimumDiversity(b, 10))
			case domain.TaskApplicationQuestions:
				require.NoError(t, AssertMinimumDiversity(b, 5))
			}
			if task == domain.TaskFormVision {
				for _, c := range b.Cases {
					var in struct {
						Fixture string `json:"fixture_png"`
					}
					require.NoError(t, json.Unmarshal(c.Input, &in))
					_, err := ReadFixtureBytes(c, in.Fixture)
					require.NoError(t, err, "fixture %s", in.Fixture)
				}
			}
		})
	}
}
