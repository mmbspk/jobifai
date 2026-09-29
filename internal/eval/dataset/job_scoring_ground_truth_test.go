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
	require.NoError(t, AssertMinimumDiversity(b, 10))
	for _, c := range b.Cases {
		var exp struct {
			ExpectPass bool   `json:"expect_pass"`
			ExpectSkip bool   `json:"expect_skip"`
			Scenario   string `json:"scenario"`
		}
		require.NoError(t, json.Unmarshal(c.Expect, &exp))
		require.True(t, exp.ExpectPass != exp.ExpectSkip, "case %s must label pass or skip", c.ID)
		if exp.ExpectSkip {
			require.Contains(t, []string{
				"seniority_mismatch", "mandatory_skill_mismatch", "qualification_mismatch",
				"location_mismatch", "insufficient_experience", "overqualified", "domain_mismatch",
				"tech_mismatch", "education_level_mismatch", "licence_mismatch",
			}, exp.Scenario, "skip case %s needs mismatch scenario", c.ID)
		}
	}
}
