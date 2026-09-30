package recommend

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/eval/candidate"
)

func TestAggregate_NonScoringTaskOmitsNaNAccuracy(t *testing.T) {
	t.Parallel()
	m := Aggregate(candidate.Spec{Provider: "claude", Model: "m"}, 2, 2, 0, []int64{10, 20}, []int64{100, 100}, 0, 0, 0, 0)
	require.Zero(t, m.ScoringAccuracy)
	b, err := json.Marshal(m)
	require.NoError(t, err)
	require.NotContains(t, string(b), "NaN")
}

func TestAggregate_CoverLetterMetricsMarshal(t *testing.T) {
	t.Parallel()
	m := Aggregate(candidate.Spec{Provider: "claude", Model: "m"}, 5, 5, 0, []int64{1, 2, 3, 4, 5}, []int64{1, 1, 1, 1, 1}, 0, 0, 0, 0)
	s, err := MarshalJSON(m)
	require.NoError(t, err)
	require.NotContains(t, s, "NaN")
	require.NotContains(t, s, "Inf")
}

func TestMarshalJSON_RejectsNaN(t *testing.T) {
	t.Parallel()
	_, err := MarshalJSON(map[string]float64{"bad": math.NaN()})
	require.Error(t, err)
}
