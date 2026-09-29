package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/eval/candidate"
	"github.com/user/jobifai/internal/eval/dataset"
	"github.com/user/jobifai/internal/llm"
)

// FakeRunner returns deterministic outputs for CI (no external APIs).
type FakeRunner struct{}

func (FakeRunner) RunCase(_ context.Context, task string, spec candidate.Spec, c dataset.Case) (string, llm.UsageObservation, error) {
	start := time.Now()
	var out string
	switch task {
	case domain.TaskJobScoring:
		var exp struct {
			MinScore int `json:"min_score"`
		}
		_ = json.Unmarshal(c.Expect, &exp)
		score := exp.MinScore
		if score == 0 {
			score = 8
		}
		out = fmt.Sprintf(`{"score":%d,"reasoning":"synthetic"}`, score)
	case domain.TaskEmploymentEthics:
		var exp struct {
			Verdict string `json:"verdict"`
		}
		_ = json.Unmarshal(c.Expect, &exp)
		v := exp.Verdict
		if v == "" {
			v = "HALAL"
		}
		out = fmt.Sprintf(`{"verdict":%q,"confidence":"HIGH","summary":"ok","reasons":["r"],"caveats":null,"scholar_note":null}`, strings.ToUpper(v))
	case domain.TaskFormAnswer:
		var exp struct {
			Exact string `json:"exact"`
		}
		_ = json.Unmarshal(c.Expect, &exp)
		out = exp.Exact
	default:
		out = `{}`
	}
	obs := llm.UsageObservation{
		Provider: spec.Provider, RequestedModel: spec.Model, ActualModel: spec.Model,
		ActualModelVerified: true,
		LatencyMS:           time.Since(start).Milliseconds(),
		Success:             true,
		RawCostMicro:        1000,
	}
	return out, obs, nil
}
