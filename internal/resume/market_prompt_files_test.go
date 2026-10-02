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
	assert.NotContains(t, m.CoverLetterPrompt, "JSON string fields")

	mDE, err := resume.LoadMarket(filepath.Join(root, "market_germany.yaml"))
	require.NoError(t, err)
	assert.Contains(t, mDE.CoverLetterPrompt, "COVER LETTER OUTPUT")
	assert.NotContains(t, mDE.CoverLetterPrompt, "JSON string fields")
	assert.Contains(t, mDE.ResumePrompt, "JSON string fields")
	assert.Contains(t, mDE.TailoredPrompt, "Keep summary and bullets in English")
}
