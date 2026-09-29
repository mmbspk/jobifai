package recommend

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/eval/candidate"
)

func TestSelectAll_SubjectiveWritingTasksRequireManualReview(t *testing.T) {
	t.Parallel()
	bl := Metrics{Candidate: candidate.Spec{Provider: "claude", Model: "m1"}, CaseCount: 50, TotalCostMicro: 1000, DetPassRate: 0.95}
	cheap := Metrics{Candidate: candidate.Spec{Provider: "claude", Model: "m2"}, CaseCount: 50, TotalCostMicro: 400, DetPassRate: 0.96}
	for _, task := range []string{domain.TaskResumeTailoring, domain.TaskCoverLetter} {
		out := SelectAll(SelectInput{
			Task: task, Baseline: bl, Candidates: []Metrics{cheap},
			MinCases: 10, MaxCritical: 0, QualityFloor: DefaultQualityFloor,
		})
		require.Len(t, out, 1)
		require.Equal(t, OutcomeManualReview, out[0].Outcome)
		require.False(t, out[0].Deployable)
		require.Greater(t, out[0].Metrics.TotalCostMicro, int64(0))
	}
}
