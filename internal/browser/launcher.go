package browser

import (
	"os"

	"github.com/go-rod/rod/lib/defaults"
	"github.com/go-rod/rod/lib/launcher"
)

// NewLauncher uses the configured browser or installed Chrome/Chromium before
// falling back to Rod's bundled revision, which can age out of site support.
// All session and automation launches must use the same selection policy.
func NewLauncher() *launcher.Launcher {
	l := launcher.New()
	if bin := selectBrowserBinary(os.Getenv("ROD_BROWSER_BIN"), defaults.Bin, launcher.LookPath); bin != "" {
		l.Bin(bin)
	}
	return l
}

func selectBrowserBinary(configured, rodDefault string, lookPath func() (string, bool)) string {
	if configured != "" {
		return configured
	}
	if rodDefault != "" {
		return rodDefault
	}
	if bin, ok := lookPath(); ok {
		return bin
	}
	return ""
}
