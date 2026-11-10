package bot

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTruncateRunes(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "hello", truncateRunes("hello world", 5))
	assert.Equal(t, "x", truncateRunes("x", 0))
	assert.Equal(t, "ab", truncateRunes("ab", 10))
	assert.Equal(t, "", sanitizeSalaryFieldAnswer("Open to negotiation", 20))
	assert.Equal(t, "88000", sanitizeSalaryFieldAnswer("88,000 AUD per year", 20))
}
