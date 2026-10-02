package resume

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandStylesheetImports_WithAndWithoutTrailingNewline(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	base := "body { color: red; }\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "_base_a4.css"), []byte(base), 0o644))

	cases := []struct {
		name string
		css  string
	}{
		{name: "no trailing newline", css: `@import "./_base_a4.css";`},
		{name: "trailing newline", css: "@import \"./_base_a4.css\";\n"},
		{name: "leading content", css: "/* skin */\n@import \"./_base_a4.css\";\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := expandStylesheetImports(tc.css, dir)
			assert.Contains(t, got, "body { color: red; }")
			assert.NotContains(t, got, `@import "./_base_a4.css"`)
		})
	}
}

func TestExpandStylesheetImports_InvalidImportDoesNotPanic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	got := expandStylesheetImports(`@import "./missing.css";`, dir)
	assert.Equal(t, `@import "./missing.css";`, got)
}
