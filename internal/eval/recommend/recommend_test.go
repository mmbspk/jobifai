package recommend

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/eval/candidate"
)

func TestAggregate_ScoringRatesUseCorrectDenominators(t *testing.T) {
	t.Parallel()
	// 2 TP, 1 FN => FNR = 1/3; 2 TN, 1 FP => FPR = 1/3
	m := Aggregate(candidate.Spec{Provider: "claude", Model: "m"}, 4, 4, 0, []int64{1, 1, 1, 1}, []int64{1, 1, 1, 1}, 2, 2, 1, 1)
	require.InDelta(t, 1.0/3.0, m.ScoringFalseNegRate, 0.001)
	require.InDelta(t, 1.0/3.0, m.ScoringFalsePosRate, 0.001)
	require.InDelta(t, 4.0/6.0, m.ScoringAccuracy, 0.001)
}
