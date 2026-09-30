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
			ExpectPass bool   `json:"expect_pass"`
			ExpectSkip bool   `json:"expect_skip"`
			Scenario   string `json:"scenario"`
		}
		require.NoError(t, json.Unmarshal(c.Expect, &exp))
		require.True(t, exp.ExpectPass != exp.ExpectSkip, "case %s must label pass or skip", c.ID)
		if exp.ExpectSkip {
			require.True(t, exp.Scenario != "" && !exp.ExpectPass, "skip case %s must name a mismatch scenario", c.ID)
		}
	}
}
