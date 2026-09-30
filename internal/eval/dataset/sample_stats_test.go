package dataset

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
)

func TestComputeSampleStats_HardAndBorderlineEffectiveSizes(t *testing.T) {
	t.Parallel()
	cases := []Case{
		{ID: "a", Task: domain.TaskJobScoring, Input: json.RawMessage(`{"x":1}`), Expect: json.RawMessage(`{"expect_pass":true}`)},
		{ID: "b", Task: domain.TaskJobScoring, Input: json.RawMessage(`{"x":2}`), Expect: json.RawMessage(`{"expect_skip":true}`)},
		{ID: "c", Task: domain.TaskJobScoring, Input: json.RawMessage(`{"x":3}`), Expect: json.RawMessage(`{"expect_borderline":true}`)},
	}
	stats := ComputeSampleStats(cases)
	require.Equal(t, 3, stats.TotalUniqueInputCount)
	require.Equal(t, 2, stats.HardEffectiveSampleSize)
	require.Equal(t, 1, stats.BorderlineEffectiveSampleSize)
}

func TestJobScoringFullDataset_HardBorderlineEffectiveN(t *testing.T) {
	t.Parallel()
	b, err := Load(LoadRequest{Task: domain.TaskJobScoring, Version: "full", Source: SourceSynthetic})
	require.NoError(t, err)
	stats := ComputeSampleStats(b.Cases)
	require.Equal(t, 60, stats.TotalUniqueInputCount)
	require.Equal(t, 57, stats.HardEffectiveSampleSize)
	require.Equal(t, 3, stats.BorderlineEffectiveSampleSize)
}
