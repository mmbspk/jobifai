package tasks

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/eval/dataset"
)

func TestConservativeBillableTokens_LargePromptExceedsFixed512Estimate(t *testing.T) {
	t.Parallel()
	longJD := strings.Repeat("commercial construction scope detail. ", 400)
	in := map[string]any{
		"job_description": longJD,
		"profile": map[string]any{
			"experience_details": []map[string]any{{
				"company": "BuildRight", "position": "Project Manager", "employment_period": "2013 – Present",
			}},
			"skills": []string{"construction delivery", "budget control"},
		},
	}
	raw, _ := json.Marshal(in)
	c := dataset.Case{ID: "job_scoring-0013", Task: domain.TaskJobScoring, Input: raw}
	tok := ConservativeBillableTokens(domain.TaskJobScoring, c, 8192)
	fixed512 := int64(512)
	require.Greater(t, tok.InputTokens, fixed512)
}
