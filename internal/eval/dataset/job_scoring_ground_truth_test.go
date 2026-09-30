package dataset

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
)

func TestJobScoringFullDataset_PassSkipSemantics(t *testing.T) {
	t.Parallel()
	b, err := Load(LoadRequest{Task: domain.TaskJobScoring, Version: "full", Source: SourceSynthetic})
	require.NoError(t, err)
	require.NoError(t, AssertMinimumDiversity(b, 40))
	for _, c := range b.Cases {
		var exp struct {
			ExpectPass       bool   `json:"expect_pass"`
			ExpectSkip       bool   `json:"expect_skip"`
			ExpectBorderline bool   `json:"expect_borderline"`
			Scenario         string `json:"scenario"`
		}
		require.NoError(t, json.Unmarshal(c.Expect, &exp))
		labels := 0
		if exp.ExpectPass {
			labels++
		}
		if exp.ExpectSkip {
			labels++
		}
		if exp.ExpectBorderline {
			labels++
		}
		require.Equal(t, 1, labels, "case %s must have exactly one of pass/skip/borderline", c.ID)
		require.NotEmpty(t, exp.Scenario)
	}
}
