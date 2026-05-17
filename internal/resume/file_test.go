package resume_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/resume"
)

func TestTextFromReader_PlainText(t *testing.T) {
	text, err := resume.TextFromReader(strings.NewReader("hello world"), "resume.txt")
	require.NoError(t, err)
	assert.Equal(t, "hello world", text)
}

func TestTextFromReader_Markdown(t *testing.T) {
	text, err := resume.TextFromReader(strings.NewReader("# Heading\nBody text."), "resume.md")
	require.NoError(t, err)
	assert.Contains(t, text, "Heading")
}

func TestTextFromReader_YAML(t *testing.T) {
	text, err := resume.TextFromReader(strings.NewReader("name: Alice"), "profile.yaml")
	require.NoError(t, err)
	assert.Equal(t, "name: Alice", text)
}

func TestTextFromReader_UnknownExtension(t *testing.T) {
	text, err := resume.TextFromReader(strings.NewReader("raw content"), "file.xyz")
	require.NoError(t, err)
	assert.Equal(t, "raw content", text)
}

func TestTextFromReader_EmptyFile(t *testing.T) {
	text, err := resume.TextFromReader(strings.NewReader(""), "empty.txt")
	require.NoError(t, err)
	assert.Equal(t, "", text)
}
