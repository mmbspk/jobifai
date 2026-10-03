package documents_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/documents"
)

func TestExportApplicationUploadFile_IsolatedPerUserAndVersion(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	a, err := documents.ExportApplicationUploadFile(root, "user-a", "job-1", "ver-1", "resume", "resume.pdf", []byte("%PDF-a"))
	require.NoError(t, err)
	b, err := documents.ExportApplicationUploadFile(root, "user-b", "job-1", "ver-1", "resume", "resume.pdf", []byte("%PDF-b"))
	require.NoError(t, err)
	require.NotEqual(t, a, b)
	raw, err := os.ReadFile(a)
	require.NoError(t, err)
	require.Equal(t, []byte("%PDF-a"), raw)
	rawB, err := os.ReadFile(b)
	require.NoError(t, err)
	require.Equal(t, []byte("%PDF-b"), rawB)
	require.Contains(t, filepath.Base(filepath.Dir(a)), "ver-1")
}
