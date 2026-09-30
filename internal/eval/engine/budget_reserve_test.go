package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/eval/candidate"
	"github.com/user/jobifai/internal/eval/dataset"
	"github.com/user/jobifai/internal/eval/tasks"
	"github.com/user/jobifai/internal/pricing"
)

func TestPerCallBudgetReserve_UsesPromptSizeNotFixed512(t *testing.T) {
	svc := &Service{EvalPricing: &pricing.EvalCatalog{Approved: pricing.DefaultCatalog()}}
	longJD := strings.Repeat("scope ", 800)
	in, _ := json.Marshal(map[string]any{
		"job_description": longJD,
		"profile": map[string]any{
			"skills": []string{"a", "b"},
			"experience_details": []map[string]any{{
				"position": "Role", "company": "Co", "employment_period": "2010 – Present",
			}},
		},
	})
	c := dataset.Case{Task: domain.TaskJobScoring, Input: in}
	small := svc.perCallBudgetReserve(context.Background(), domain.TaskJobScoring, candidate.Spec{Model: "claude-sonnet-4-6", MaxTokens: 8192}, c)
	tinyIn := dataset.Case{Task: domain.TaskJobScoring, Input: json.RawMessage(`{"job_description":"x","profile":{"skills":["a"]}}`)}
	large := svc.perCallBudgetReserve(context.Background(), domain.TaskJobScoring, candidate.Spec{Model: "claude-sonnet-4-6", MaxTokens: 8192}, tinyIn)
	require.Greater(t, small, large)
}

func TestRunBudget_LargePromptReserveBlocksConcurrentOverspend(t *testing.T) {
	t.Parallel()
	svc := &Service{EvalPricing: &pricing.EvalCatalog{Approved: pricing.DefaultCatalog()}}
	longJD := strings.Repeat("commercial scope detail. ", 500)
	in, _ := json.Marshal(map[string]any{
		"job_description": longJD,
		"profile": map[string]any{
			"skills": []string{"construction delivery", "budget control"},
			"experience_details": []map[string]any{{
				"position": "Project Manager", "company": "BuildRight", "employment_period": "2013 – Present",
			}},
		},
	})
	c := dataset.Case{Task: domain.TaskJobScoring, Input: in}
	reserve := svc.perCallBudgetReserve(context.Background(), domain.TaskJobScoring, candidate.Spec{Model: "claude-sonnet-4-6", MaxTokens: 8192}, c)
	tok512 := tasks.ConservativeBillableTokens(domain.TaskJobScoring, c, 8192)
	res, _ := pricing.DefaultCatalog().Resolve("claude-sonnet-4-6", false)
	legacy512 := pricing.RawCostMicroUSD(res.Record, pricing.TokenUsage{InputTokens: 512, OutputTokens: 8192})
	require.Greater(t, tok512.InputTokens, int64(512))
	require.Greater(t, reserve, legacy512)
	require.Greater(t, reserve, int64(1_000))
	capMicro := reserve + reserve/10
	b := newRunBudget(capMicro)
	require.True(t, b.tryReserve(reserve))
	require.False(t, b.tryReserve(reserve), "second concurrent reservation must not exceed hard cap")
	_, committed, _ := b.totals()
	require.LessOrEqual(t, committed, capMicro)
}
