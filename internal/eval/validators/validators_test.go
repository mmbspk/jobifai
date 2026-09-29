package validators

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/user/jobifai/internal/domain"
)

func TestValidate_JobScoring_FalseNegativeCritical(t *testing.T) {
	t.Parallel()
	exp, _ := json.Marshal(map[string]any{"expect_pass": true, "pass_threshold": 7})
	res := Validate(domain.TaskJobScoring, `{"score":3,"reasoning":"weak"}`, exp, true)
	assert.False(t, res.Pass)
	assert.True(t, res.CriticalFail)
}

func TestValidate_FormAnswer_Exact(t *testing.T) {
	t.Parallel()
	exp, _ := json.Marshal(map[string]any{"exact": "Yes"})
	res := Validate(domain.TaskFormAnswer, "Yes", exp, false)
	assert.True(t, res.Pass)
}
