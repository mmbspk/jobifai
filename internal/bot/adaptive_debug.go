package bot

import (
	"os"
	"path/filepath"
	"time"

	"github.com/go-rod/rod"
)

// Raw page captures are opt-in operator diagnostics, separate from reusable
// snapshots. Keep them in private temporary directories and never in the repo.
func saveAdaptiveDiagnostic(page *rod.Page, platform string) string {
	root := os.Getenv("JOBIFAI_APPLY_DEBUG_DIR")
	if root == "" || !filepath.IsAbs(root) || page == nil {
		return ""
	}
	dir, err := os.MkdirTemp(root, platform+"-")
	if err != nil {
		return ""
	}
	captured := false
	if html, err := page.Timeout(5 * time.Second).HTML(); err == nil {
		captured = os.WriteFile(filepath.Join(dir, "page.html"), []byte(html), 0o600) == nil
	}
	if shot, err := page.Timeout(5*time.Second).Screenshot(false, nil); err == nil {
		captured = os.WriteFile(filepath.Join(dir, "page.png"), shot, 0o600) == nil || captured
	}
	if !captured {
		_ = os.Remove(dir)
		return ""
	}
	return dir
}
