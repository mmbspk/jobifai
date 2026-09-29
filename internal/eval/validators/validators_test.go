package validators

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/eval/candidate"
	"github.com/user/jobifai/internal/eval/recommend"
)

func TestJobScoring_ScoringCellIgnoresScoreRangeOnlyFailure(t *testing.T) {
	t.Parallel()
	exp, _ := json.Marshal(map[string]any{"expect_pass": true, "pass_threshold": 7, "min_score": 8})
	// score 7 passes classification but fails min_score
	out := `{"score":7}`
	res := Validate(domain.TaskJobScoring, out, exp, false)
	require.Equal(t, "tp", res.Metrics["scoring_cell"])
	require.False(t, res.Pass)
}

func TestAggregate_NonScoringTaskNoNaN(t *testing.T) {
	t.Parallel()
	m := recommend.Aggregate(candidate.Spec{Provider: "claude", Model: "m"}, 3, 3, 0, []int64{1, 1, 1}, []int64{1, 1, 1}, 0, 0, 0, 0)
	b, err := recommend.MarshalJSON(m)
	require.NoError(t, err)
	require.NotContains(t, b, "NaN")
	require.Zero(t, m.ScoringAccuracy)
}

func TestResumeExtract_InventedEmployerCritical(t *testing.T) {
	t.Parallel()
	exp, _ := json.Marshal(map[string]any{
		"personal":  map[string]any{"full_name": "Alex Nurse"},
		"employers": []map[string]any{{"company": "Regional Health", "title": "Nurse"}},
	})
	out := `{"personal_information":{"name":"Alex"},"experience_details":[{"company":"Invented Co","position":"Nurse"}]}`
	res := Validate(domain.TaskResumeExtract, out, exp, true)
	require.True(t, res.CriticalFail)
	require.Greater(t, res.Metrics["invented_facts"].(int), 0)
}

func TestApplicationQuestions_YearsCountAcceptsDurationRejectsStartYear(t *testing.T) {
	t.Parallel()
	exp, _ := json.Marshal(map[string]any{
		"answer_count": 1,
		"answers": []map[string]any{
			{"question": "How many years of nursing experience do you have?", "match": "years_count", "value": "7"},
		},
	})
	for _, passOut := range []string{
		`[{"question":"How many years of nursing experience do you have?","answer":"7 years"}]`,
		`[{"question":"How many years of nursing experience do you have?","answer":"7"}]`,
	} {
		res := Validate(domain.TaskApplicationQuestions, passOut, exp, true)
		require.True(t, res.Pass, res.Errors)
	}

	failOut := `[{"question":"How many years of nursing experience do you have?","answer":"2019"}]`
	resFail := Validate(domain.TaskApplicationQuestions, failOut, exp, true)
	require.False(t, resFail.Pass)
}

func TestApplicationQuestions_SwappedOrderFails(t *testing.T) {
	t.Parallel()
	exp, _ := json.Marshal(map[string]any{
		"answer_count": 2,
		"answers": []map[string]any{
			{"question": "Q1", "match": "exact", "value": "Yes"},
			{"question": "Q2", "match": "exact", "value": "No"},
		},
	})
	out := `[{"question":"Q2","answer":"No"},{"question":"Q1","answer":"Yes"}]`
	res := Validate(domain.TaskApplicationQuestions, out, exp, true)
	require.False(t, res.Pass)
}

func TestMarshalJSON_NoNaNInMetrics(t *testing.T) {
	t.Parallel()
	m := map[string]any{"x": math.NaN()}
	_, err := recommend.MarshalJSON(m)
	require.Error(t, err)
}

func TestResumeExtract_EmployerCorrectTitleWrong(t *testing.T) {
	t.Parallel()
	exp, _ := json.Marshal(map[string]any{
		"employers": []map[string]any{{"company": "Regional Health", "title": "Registered Nurse"}},
	})
	out := `{"experience_details":[{"company":"Regional Health","position":"Director"}]}`
	res := Validate(domain.TaskResumeExtract, out, exp, false)
	require.False(t, res.Pass)
	require.Greater(t, res.Metrics["incorrect_facts"].(int), 0)
}

func TestResumeTailoring_InventedKubernetes(t *testing.T) {
	t.Parallel()
	exp, _ := json.Marshal(map[string]any{"forbidden_terms": []string{"kubernetes"}})
	out := `{"skills":["Java","Kubernetes"],"experience_details":[{"company":"Harbor Logistics","position":"Analyst"}]}`
	res := Validate(domain.TaskResumeTailoring, out, exp, true)
	require.False(t, res.Pass)
	require.True(t, res.CriticalFail)
}
