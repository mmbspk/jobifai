package documents_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/documents"
)

func repoPaths(t *testing.T) (stylesDir, marketDir string) {
	t.Helper()
	root := filepath.Join("..", "..")
	return filepath.Join(root, documents.StylesDirRelative), filepath.Join(root, "resume_markets")
}

func TestEffectiveStylesheet_TwoStylesDiffer(t *testing.T) {
	t.Parallel()
	stylesDir, marketDir := repoPaths(t)
	_, us, err := documents.EffectiveStylesheet(stylesDir, marketDir, "", "us")
	require.NoError(t, err)
	_, uk, err := documents.EffectiveStylesheet(stylesDir, marketDir, "", "uk")
	require.NoError(t, err)
	require.NotEmpty(t, us)
	require.NotEmpty(t, uk)
	require.NotEqual(t, us, uk)
}

func TestEffectiveStylesheet_EmptyStyleUsesMarketCSS(t *testing.T) {
	t.Parallel()
	stylesDir, marketDir := repoPaths(t)
	_, withStyle, err := documents.EffectiveStylesheet(stylesDir, marketDir, "US", "us")
	require.NoError(t, err)
	_, marketOnly, err := documents.EffectiveStylesheet(stylesDir, marketDir, "US", "")
	require.NoError(t, err)
	require.NotEmpty(t, marketOnly)
	require.NotEqual(t, withStyle, marketOnly)
}
