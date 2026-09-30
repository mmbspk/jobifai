package recommend

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/eval/candidate"
)

func TestConfidenceLabel_UsesEffectiveSampleSizeNotRawCount(t *testing.T) {
	t.Parallel()
	require.Equal(t, "low", confidenceLabel(20, 0.96))
	require.Equal(t, "medium", confidenceLabel(30, 0.91))
	require.Equal(t, "high", confidenceLabel(80, 0.96))

	base := Metrics{
		Candidate: candidate.Spec{Provider: "claude", Model: "claude-sonnet-4-6"},
		CaseCount: 80, EffectiveSampleSize: 20, DetPassRate: 0.96, TotalCostMicro: 1000, CoverageComplete: true,
	}
	cand := Metrics{
		Candidate: candidate.Spec{Provider: "claude", Model: "claude-haiku-4-5-20251001"},
		CaseCount: 80, EffectiveSampleSize: 20, DetPassRate: 0.97, TotalCostMicro: 300, CoverageComplete: true,
	}
	rec := evaluateOne(SelectInput{
		Task: "form_answer", MinCases: 10, MaxCritical: 0, EffectiveSampleSize: 20,
		Baseline: base, QualityFloor: DefaultQualityFloor,
	}, cand)
	require.Equal(t, OutcomeRecommend, rec.Outcome)
	require.Equal(t, "low", rec.Confidence, "confidence must follow effective N=20, not raw 80")
}
