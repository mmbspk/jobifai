package resume_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/resume"
)

const minimalMarketYAML = `name: TestMarket
resume_prompt: "Write a great resume."
tailored_prompt: "Tailor for the job."
cover_letter_prompt: "Write a cover letter."
`

func TestLoadMarket_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "market_test.yaml")
	require.NoError(t, os.WriteFile(path, []byte(minimalMarketYAML), 0o644))

	m, err := resume.LoadMarket(path)
	require.NoError(t, err)
	assert.Equal(t, "TestMarket", m.Name)
	assert.Equal(t, "Write a great resume.", m.ResumePrompt)
	assert.Equal(t, "Tailor for the job.", m.TailoredPrompt)
}

func TestLoadMarket_Missing(t *testing.T) {
	_, err := resume.LoadMarket("/nonexistent/path/market.yaml")
	require.Error(t, err)
}

func TestLoadMarketByName_Found(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "market_generic.yaml"), []byte(minimalMarketYAML), 0o644))

	m := resume.LoadMarketByName(dir, "TestMarket")
	require.NotNil(t, m)
	assert.Equal(t, "TestMarket", m.Name)
}

func TestLoadMarketByName_CaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "market_generic.yaml"), []byte(minimalMarketYAML), 0o644))

	m := resume.LoadMarketByName(dir, "testmarket")
	require.NotNil(t, m)
}

func TestLoadMarketByName_NotFound(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "market_generic.yaml"), []byte(minimalMarketYAML), 0o644))

	m := resume.LoadMarketByName(dir, "DoesNotExist")
	assert.Nil(t, m)
}

func TestLoadMarketByName_EmptyDir(t *testing.T) {
	m := resume.LoadMarketByName(t.TempDir(), "TestMarket")
	assert.Nil(t, m)
}

func TestLoadMarketByName_EmptyArgs(t *testing.T) {
	assert.Nil(t, resume.LoadMarketByName("", "TestMarket"))
	assert.Nil(t, resume.LoadMarketByName("/some/dir", ""))
}
