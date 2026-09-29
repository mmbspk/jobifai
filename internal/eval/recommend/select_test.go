package recommend

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/eval/candidate"
)

func baseMetrics(c candidate.Spec, cases int, cost int64, det float64, crit int, fnRate float64) Metrics {
	return Metrics{
		Candidate: c, CaseCount: cases, TotalCostMicro: cost, DetPassRate: det,
		CriticalFails: crit, ScoringFalseNegRate: fnRate,
	}
}

func TestSelectAll_CheaperEquivalentQualityRecommends(t *testing.T) {
	t.Parallel()
	bl := baseMetrics(candidate.Spec{Provider: "claude", Model: "m1"}, 50, 1000, 0.9, 0, 0.02)
	cheap := baseMetrics(candidate.Spec{Provider: "claude", Model: "m2"}, 50, 500, 0.91, 0, 0.02)
	out := SelectAll(SelectInput{
		Task: "job_scoring", Baseline: bl, Candidates: []Metrics{cheap},
		MinCases: 10, MaxCritical: 0, QualityFloor: DefaultQualityFloor,
	})
	require.Len(t, out, 1)
	require.Equal(t, OutcomeRecommend, out[0].Outcome)
	require.True(t, out[0].Deployable)
}

func TestSelectAll_CriticalFalseNegativeRejects(t *testing.T) {
	t.Parallel()
	bl := baseMetrics(candidate.Spec{Provider: "claude", Model: "m1"}, 50, 1000, 0.9, 0, 0.01)
	bad := baseMetrics(candidate.Spec{Provider: "claude", Model: "m2"}, 50, 400, 0.9, 0, 0.05)
	out := SelectAll(SelectInput{
		Task: "job_scoring", Baseline: bl, Candidates: []Metrics{bad},
		MinCases: 10, MaxCritical: 0, QualityFloor: DefaultQualityFloor,
	})
	require.Equal(t, OutcomeKeepIncumbent, out[0].Outcome)
	require.False(t, out[0].Deployable)
}

func TestSelectAll_MoreExpensiveEquivalentKeepIncumbent(t *testing.T) {
	t.Parallel()
	bl := baseMetrics(candidate.Spec{Provider: "claude", Model: "m1"}, 50, 500, 0.9, 0, 0)
	expensive := baseMetrics(candidate.Spec{Provider: "claude", Model: "m2"}, 50, 900, 0.91, 0, 0)
	out := SelectAll(SelectInput{
		Task: "form_answer", Baseline: bl, Candidates: []Metrics{expensive},
		MinCases: 10, MaxCritical: 0, QualityFloor: DefaultQualityFloor,
	})
	require.Equal(t, OutcomeKeepIncumbent, out[0].Outcome)
}

func TestSelectAll_SmokeOrFakeNotDeployable(t *testing.T) {
	t.Parallel()
	bl := baseMetrics(candidate.Spec{Provider: "claude", Model: "m1"}, 50, 1000, 0.9, 0, 0)
	cheap := baseMetrics(candidate.Spec{Provider: "claude", Model: "m2"}, 50, 100, 0.95, 0, 0)
	out := SelectAll(SelectInput{
		Task: "form_answer", Baseline: bl, Candidates: []Metrics{cheap},
		MinCases: 3, SmokeOrFake: true,
	})
	require.Equal(t, OutcomeNeedsMoreData, out[0].Outcome)
	require.False(t, out[0].Deployable)
}

func TestSelectAll_CrossProviderNotDeployable(t *testing.T) {
	t.Parallel()
	bl := baseMetrics(candidate.Spec{Provider: "claude", Model: "m1"}, 50, 1000, 0.95, 0, 0)
	other := baseMetrics(candidate.Spec{Provider: "openai", Model: "gpt-x"}, 50, 400, 0.96, 0, 0)
	out := SelectAll(SelectInput{
		Task: "form_answer", Baseline: bl, Candidates: []Metrics{other},
		MinCases: 10, MaxCritical: 0, QualityFloor: DefaultQualityFloor,
	})
	require.Equal(t, OutcomeNotDeployableCrossProv, out[0].Outcome)
}
