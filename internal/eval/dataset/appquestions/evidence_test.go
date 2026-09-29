package appquestions_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/eval/dataset"
	"github.com/user/jobifai/internal/eval/dataset/appquestions"
	"github.com/user/jobifai/internal/llm"
	"github.com/user/jobifai/internal/resume"
)

func TestApplicationQuestionsScenarios_EvidenceInProviderMessages(t *testing.T) {
	t.Parallel()
	for _, s := range appquestions.Scenarios() {
		t.Run(s.Tag, func(t *testing.T) {
			t.Parallel()
			in := scenarioInput(t, s)
			msgs, err := resume.ProviderMessages(domain.TaskApplicationQuestions, in)
			require.NoError(t, err)
			blob := strings.ToLower(messagesBlob(msgs))
			for _, exp := range s.Expect {
				if isSalary(exp.Value) {
					require.Contains(t, blob, strings.ToLower(exp.Value), "salary evidence")
				}
				if strings.HasSuffix(exp.Value, " weeks") {
					require.Contains(t, blob, strings.ToLower(exp.Value), "notice evidence")
				}
				if exp.Contains == "2019" {
					require.Contains(t, blob, "2019", "employment period evidence")
				}
				if strings.Contains(strings.ToLower(exp.Question), "sponsor") && exp.Value == "Yes" {
					require.Contains(t, blob, "requires_sponsorship")
				}
			}
		})
	}
}

func TestApplicationQuestionsFullDataset_EvidenceSurvival(t *testing.T) {
	t.Parallel()
	b, err := dataset.Load(dataset.LoadRequest{Task: domain.TaskApplicationQuestions, Version: "full", Source: dataset.SourceSynthetic})
	require.NoError(t, err)
	for _, c := range b.Cases {
		msgs, err := resume.ProviderMessages(domain.TaskApplicationQuestions, c.Input)
		require.NoError(t, err)
		blob := strings.ToLower(messagesBlob(msgs))
		var exp struct {
			Answers []struct {
				Match, Value, Contains string
			} `json:"answers"`
		}
		require.NoError(t, json.Unmarshal(c.Expect, &exp))
		for _, a := range exp.Answers {
			if isSalary(a.Value) {
				require.Contains(t, blob, strings.ToLower(a.Value), "case %s", c.ID)
			}
			if a.Contains != "" {
				require.Contains(t, blob, strings.ToLower(a.Contains), "case %s", c.ID)
			}
			if strings.HasSuffix(a.Value, " weeks") {
				require.Contains(t, blob, strings.ToLower(a.Value), "case %s", c.ID)
			}
		}
	}
}

func scenarioInput(t *testing.T, s appquestions.Scenario) json.RawMessage {
	t.Helper()
	profile, err := appquestions.ProfileJSON(s.Profile)
	require.NoError(t, err)
	qs := make([]map[string]string, len(s.Questions))
	for i, q := range s.Questions {
		qs[i] = map[string]string{"id": q.ID, "text": q.Text}
	}
	out, err := json.Marshal(map[string]any{
		"profile": profile, "job_context": s.JobCtx, "questions": qs,
	})
	require.NoError(t, err)
	return out
}

func messagesBlob(msgs []llm.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		b.WriteString(m.Content)
	}
	return b.String()
}

func isSalary(v string) bool {
	if v == "" {
		return false
	}
	for _, r := range v {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(v) >= 4
}
