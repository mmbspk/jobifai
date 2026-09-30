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
	case domain.TaskFormVision:
		return validateFormVision(output, expect, critical)
	case domain.TaskResumeExtract:
		return validateResumeExtract(output, expect, critical)
	case domain.TaskResumeTailoring:
		return validateResumeTailoring(output, expect, critical)
	case domain.TaskCoverLetter:
		return validateCoverLetter(output, expect, critical)
	case domain.TaskApplicationQuestions:
		return validateApplicationQuestions(output, expect, critical)
	default:
		return Result{Errors: []string{"unknown task validator"}}
	}
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
	predictedPass := js.Score >= exp.PassThreshold
	if exp.ExpectPass && !predictedPass {
		res.Errors = append(res.Errors, "false negative: expected pass")
		res.CriticalFail = critical
	}
	if exp.ExpectSkip && predictedPass {
		res.Errors = append(res.Errors, "false positive: expected skip")
	}
	res.Pass = len(res.Errors) == 0
	if res.Pass {
		res.DeterministicScore = 1
	}
	res.Metrics = map[string]any{"score_in_range": res.Pass, "predicted_pass": predictedPass}
	switch {
	case exp.ExpectPass && predictedPass:
		res.Metrics["scoring_cell"] = "tp"
	case exp.ExpectPass && !predictedPass:
		res.Metrics["scoring_cell"] = "fn"
	case exp.ExpectSkip && !predictedPass:
		res.Metrics["scoring_cell"] = "tn"
	case exp.ExpectSkip && predictedPass:
		res.Metrics["scoring_cell"] = "fp"
	}
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
	Match    string `json:"match"`
	Exact    string `json:"exact"`
	Contains string `json:"contains"`
	Level    string `json:"level"`
	Field    string `json:"field"`
}

func validateFormAnswer(output string, expect json.RawMessage, critical bool) Result {
	var exp formExpect
	_ = json.Unmarshal(expect, &exp)
	got := strings.TrimSpace(output)
	res := Result{}
	match := exp.Match
	if match == "" {
		if exp.Exact != "" {
			match = "exact_option"
		} else if exp.Contains != "" {
			match = "contains_fact"
		}
	}
	ok := false
	switch match {
	case "exact_option", "exact":
		ok = matchExactOption(got, exp.Exact)
	case "qualification":
		ok = matchQualification(got, exp.Level, exp.Field)
		if !ok && exp.Exact != "" {
			ok = matchQualification(got, exp.Exact, exp.Field)
		}
	case "contains_fact", "contains":
		ok = matchContainsFact(got, exp.Contains)
	case "numeric":
		ok = matchCurrencyAmount(got, exp.Exact)
	default:
		if exp.Exact != "" {
			ok = matchExactOption(got, exp.Exact)
		}
	}
	if !ok {
		res.Errors = append(res.Errors, fmt.Sprintf("answer mismatch for match=%s", match))
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
