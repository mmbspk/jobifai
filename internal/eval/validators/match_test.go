package validators

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
)

func TestResumeExtract_AlexReedIdentity(t *testing.T) {
	t.Parallel()
	exp, _ := json.Marshal(map[string]any{
		"personal_information": map[string]any{"name": "Alex", "surname": "Reed"},
	})
	pass := `{"personal_information":{"name":"Alex","surname":"Reed"}}`
	res := Validate(domain.TaskResumeExtract, pass, exp, true)
	require.True(t, res.Pass, res.Errors)

	fail := `{"personal_information":{"name":"Alex","surname":"Nguyen"}}`
	resFail := Validate(domain.TaskResumeExtract, fail, exp, true)
	require.False(t, resFail.Pass)
}

func TestFormAnswer_QualificationAcceptsProse(t *testing.T) {
	t.Parallel()
	exp, _ := json.Marshal(map[string]any{"match": "qualification", "level": "Bachelor", "field": "Nursing"})
	res := Validate(domain.TaskFormAnswer, "Bachelor of Nursing", exp, false)
	require.True(t, res.Pass, res.Errors)
	resFail := Validate(domain.TaskFormAnswer, "Diploma of Nursing", exp, false)
	require.False(t, resFail.Pass)
}

func TestApplicationQuestions_BooleanAndCurrency(t *testing.T) {
	t.Parallel()
	exp, _ := json.Marshal(map[string]any{
		"answer_count": 2,
		"answers": []map[string]any{
			{"question": "Do you require sponsorship?", "match": "boolean_yes", "value": "true"},
			{"question": "Salary expectation?", "match": "currency_amount", "value": "88000 AUD"},
		},
	})
	out := `[{"question":"Do you require sponsorship?","answer":"Yes, I require employer sponsorship."},{"question":"Salary expectation?","answer":"My salary expectation is AUD 88,000."}]`
	res := Validate(domain.TaskApplicationQuestions, out, exp, true)
	require.True(t, res.Pass, res.Errors)

	fail := `[{"question":"Do you require sponsorship?","answer":"No"},{"question":"Salary expectation?","answer":"88000 AUD"}]`
	resFail := Validate(domain.TaskApplicationQuestions, fail, exp, true)
	require.False(t, resFail.Pass)
}
