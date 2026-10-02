package resume_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/resume"
)

func TestLoadMarket_PromptFilesUnitedStates(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "resume_markets")
	m, err := resume.LoadMarket(filepath.Join(root, "market_united_states.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "United States", m.Name)
	assert.Equal(t, "en-US", m.Locale)
	assert.Contains(t, m.ResumePrompt, "United States job market")
	assert.Contains(t, m.ResumePrompt, "ATS & EXTRACTION")
}
