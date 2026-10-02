package documents_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/documents"
)

func TestEffectiveStylesheet_TwoStylesDiffer(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..")
	stylesDir := filepath.Join(root, "resume_markets", "styles")
	marketDir := filepath.Join(root, "resume_markets")
	_, us, err := documents.EffectiveStylesheet(stylesDir, marketDir, "", "us")
	require.NoError(t, err)
	_, uk, err := documents.EffectiveStylesheet(stylesDir, marketDir, "", "uk")
	require.NoError(t, err)
	require.NotEmpty(t, us)
	require.NotEmpty(t, uk)
	require.NotEqual(t, us, uk)
}
