package tasks_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/eval/dataset"
	"github.com/user/jobifai/internal/eval/tasks"
	"github.com/user/jobifai/internal/resume"
)

func TestPromptParity_EvalMatchesProductionProviderMessages(t *testing.T) {
	t.Parallel()
	taskList := []string{
		domain.TaskJobScoring, domain.TaskEmploymentEthics, domain.TaskFormAnswer, domain.TaskFormVision,
		domain.TaskResumeExtract, domain.TaskResumeTailoring, domain.TaskCoverLetter, domain.TaskApplicationQuestions,
	}
	for _, task := range taskList {
		t.Run(task, func(t *testing.T) {
			t.Parallel()
			b, err := dataset.Load(dataset.LoadRequest{Task: task, Version: "smoke", Source: dataset.SourceSynthetic})
			require.NoError(t, err)
			require.NotEmpty(t, b.Cases)
			for _, c := range b.Cases {
				prod, err := resume.ProviderMessages(task, c.Input)
				require.NoError(t, err)
				eval, _, err := tasks.MessagesForCase(task, c)
				require.NoError(t, err)
				require.Equal(t, prod, eval, "case %s", c.ID)
			}
		})
	}
}
