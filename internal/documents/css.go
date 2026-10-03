package documents

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/user/jobifai/internal/resume"
)

// EffectiveStylesheet resolves the CSS the PDF renderer would use (style name, then market CSS, then built-in default).
// cssOverridePath is non-empty when a concrete file path was used (for provenance).
func EffectiveStylesheet(stylesDir, marketDir, market, styleName string) (cssOverridePath, cssSnapshot string, err error) {
	if css, path, ok := loadStyleCSS(stylesDir, styleName); ok {
		return path, css, nil
	}
	if market != "" && marketDir != "" {
		m := resume.LoadMarketByName(marketDir, market)
		if m != nil && m.CSSFile != "" {
			data, readErr := os.ReadFile(m.CSSFile)
			if readErr == nil {
				snap := resume.ExpandStylesheetImports(string(data), filepath.Dir(m.CSSFile))
				return m.CSSFile, snap, nil
			}
		}
	}
	return "", resume.DefaultPDFCSS(), nil
}

func loadStyleCSS(stylesDir, styleName string) (css, path string, ok bool) {
	if stylesDir == "" || strings.TrimSpace(styleName) == "" {
		return "", "", false
	}
	entries, err := os.ReadDir(stylesDir)
	if err != nil {
		return "", "", false
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".css") {
			continue
		}
		stem := strings.TrimSuffix(e.Name(), ".css")
		name := strings.ReplaceAll(strings.TrimPrefix(stem, "style_"), "_", " ")
		if styleName != "" && !strings.EqualFold(name, styleName) {
			continue
		}
		full := filepath.Join(stylesDir, e.Name())
		data, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		return resume.ExpandStylesheetImports(string(data), filepath.Dir(full)), full, true
	}
	return "", "", false
}
