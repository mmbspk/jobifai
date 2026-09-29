package validators

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/user/jobifai/internal/domain"
)

// Result from deterministic validation of one case output.
type Result struct {
	Pass               bool
	DeterministicScore float64
	Errors             []string
	CriticalFail       bool
	Metrics            map[string]any
}

var productionTasks = map[string]bool{
	domain.TaskJobScoring: true, domain.TaskEmploymentEthics: true,
	domain.TaskResumeExtract: true, domain.TaskResumeTailoring: true,
	domain.TaskCoverLetter: true, domain.TaskFormAnswer: true,
	domain.TaskFormVision: true, domain.TaskApplicationQuestions: true,
}

// HasValidator reports whether eval can run for a task.
func HasValidator(task string) bool {
	return productionTasks[task]
}

// Validate runs task-specific deterministic checks.
func Validate(task, output string, expect json.RawMessage, critical bool) Result {
	if !HasValidator(task) {
		return Result{Errors: []string{"no validator"}}
	}
	switch task {
	case domain.TaskJobScoring:
		return validateJobScoring(output, expect, critical)
	case domain.TaskEmploymentEthics:
		return validateEmploymentEthics(output, expect, critical)
	case domain.TaskFormAnswer:
		return validateFormAnswer(output, expect, critical)
	case domain.TaskResumeExtract, domain.TaskResumeTailoring, domain.TaskCoverLetter, domain.TaskFormVision, domain.TaskApplicationQuestions:
		return validateGenericJSON(output, expect, critical)
	default:
		return Result{Errors: []string{"unknown task validator"}}
	}
}

func validateGenericJSON(output string, expect json.RawMessage, critical bool) Result {
	var exp struct {
		NonEmpty bool `json:"non_empty"`
	}
	_ = json.Unmarshal(expect, &exp)
	res := Result{Metrics: map[string]any{}}
	raw := stripJSON(output)
	if exp.NonEmpty {
		res.Pass = strings.TrimSpace(raw) != "" && raw != "{}"
	} else {
		var js any
		res.Pass = json.Unmarshal([]byte(raw), &js) == nil
	}
	if !res.Pass && critical {
		res.CriticalFail = true
	}
	if res.Pass {
		res.DeterministicScore = 1
	} else {
		res.Errors = append(res.Errors, "validation failed")
	}
	return res
}

type scoringExpect struct {
	MinScore      int  `json:"min_score"`
	MaxScore      int  `json:"max_score"`
	ExpectPass    bool `json:"expect_pass"`
	ExpectSkip    bool `json:"expect_skip"`
	PassThreshold int  `json:"pass_threshold"`
}

func validateJobScoring(output string, expect json.RawMessage, critical bool) Result {
	var exp scoringExpect
	_ = json.Unmarshal(expect, &exp)
	if exp.PassThreshold == 0 {
		exp.PassThreshold = 7
	}
	raw := stripJSON(output)
	var js struct {
		Score int `json:"score"`
	}
	res := Result{DeterministicScore: 0}
	if err := json.Unmarshal([]byte(raw), &js); err != nil {
		res.Errors = append(res.Errors, "invalid json")
		res.CriticalFail = critical
		return res
	}
	if js.Score < 0 || js.Score > 10 {
		res.Errors = append(res.Errors, "score out of range")
	}
	if exp.MinScore > 0 && js.Score < exp.MinScore {
		res.Errors = append(res.Errors, fmt.Sprintf("score %d below min %d", js.Score, exp.MinScore))
	}
	if exp.MaxScore > 0 && js.Score > exp.MaxScore {
		res.Errors = append(res.Errors, fmt.Sprintf("score %d above max %d", js.Score, exp.MaxScore))
	}
	if exp.ExpectPass && js.Score < exp.PassThreshold {
		res.Errors = append(res.Errors, "false negative: expected pass")
		res.CriticalFail = critical
		res.Metrics = map[string]any{"scoring_false_negative": true}
	}
	if exp.ExpectSkip && js.Score >= exp.PassThreshold {
		res.Errors = append(res.Errors, "false positive: expected skip")
		res.Metrics = map[string]any{"scoring_false_positive": true}
	}
	res.Pass = len(res.Errors) == 0
	if res.Pass {
		res.DeterministicScore = 1
	}
	if res.Metrics == nil {
		res.Metrics = map[string]any{}
	}
	res.Metrics["score_in_range"] = res.Pass
	return res
}

type ethicsExpect struct {
	Verdict string `json:"verdict"`
}

func validateEmploymentEthics(output string, expect json.RawMessage, critical bool) Result {
	var exp ethicsExpect
	_ = json.Unmarshal(expect, &exp)
	raw := stripJSON(output)
	var js struct {
		Verdict string `json:"verdict"`
	}
	res := Result{}
	if err := json.Unmarshal([]byte(raw), &js); err != nil {
		res.Errors = append(res.Errors, "invalid json")
		res.CriticalFail = critical
		return res
	}
	js.Verdict = strings.ToUpper(strings.TrimSpace(js.Verdict))
	exp.Verdict = strings.ToUpper(strings.TrimSpace(exp.Verdict))
	if exp.Verdict != "" && js.Verdict != exp.Verdict {
		res.Errors = append(res.Errors, fmt.Sprintf("verdict got %s want %s", js.Verdict, exp.Verdict))
		if critical {
			res.CriticalFail = true
		}
	}
	res.Pass = len(res.Errors) == 0
	if res.Pass {
		res.DeterministicScore = 1
	}
	return res
}

type formExpect struct {
	Exact string `json:"exact"`
}

func validateFormAnswer(output string, expect json.RawMessage, critical bool) Result {
	var exp formExpect
	_ = json.Unmarshal(expect, &exp)
	got := strings.TrimSpace(output)
	want := strings.TrimSpace(exp.Exact)
	res := Result{}
	if want != "" && got != want {
		res.Errors = append(res.Errors, fmt.Sprintf("expected %q got %q", want, got))
		if critical {
			res.CriticalFail = true
		}
	}
	res.Pass = len(res.Errors) == 0
	if res.Pass {
		res.DeterministicScore = 1
	}
	return res
}

func stripJSON(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSuffix(s, "```")
	}
	return strings.TrimSpace(s)
}
