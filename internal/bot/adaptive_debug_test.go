package bot

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdaptiveDiagnostic_OptInAndPrivatePermissions(t *testing.T) {
	page := rodTestPage(t, `<html><body><input value="private diagnostic candidate"></body></html>`)
	t.Setenv("JOBIFAI_APPLY_DEBUG_DIR", "")
	assert.Empty(t, saveAdaptiveDiagnostic(page, "seek"))
	root := t.TempDir()
	t.Setenv("JOBIFAI_APPLY_DEBUG_DIR", root)
	saved := saveAdaptiveDiagnostic(page, "seek")
	require.NotEmpty(t, saved)
	info, err := os.Stat(saved)
	require.NoError(t, err)
	assert.EqualValues(t, 0o700, info.Mode().Perm())
	_, err = os.Stat(filepath.Join(saved, "page.html"))
	require.NoError(t, err)
	files, err := os.ReadDir(saved)
	require.NoError(t, err)
	for _, file := range files {
		info, err := os.Stat(filepath.Join(saved, file.Name()))
		require.NoError(t, err)
		assert.EqualValues(t, 0o600, info.Mode().Perm())
	}
}
