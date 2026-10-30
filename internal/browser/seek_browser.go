package browser

import (
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/go-rod/rod"
)

// ErrSeekBrowserUnsupported must not be treated as a successful empty search
// or an authentication failure that can be fixed by repeatedly signing in.
var ErrSeekBrowserUnsupported = errors.New("SEEK requires a newer browser: update Chrome/Chromium, or set ROD_BROWSER_BIN to a current browser executable, then reconnect the SEEK session")

func isSeekBrowserUpgrade(pageURL string) bool {
	u, err := url.Parse(pageURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return (host == "seek.com" || strings.HasSuffix(host, ".seek.com") || host == "seek.com.au" || strings.HasSuffix(host, ".seek.com.au")) && strings.TrimRight(u.Path, "/") == "/browser-upgrade"
}

// CheckSeekBrowser rejects SEEK's explicit unsupported-browser redirect.
func CheckSeekBrowser(page *rod.Page) error {
	info, err := page.Timeout(5 * time.Second).Info()
	if err != nil {
		return err
	}
	if isSeekBrowserUpgrade(info.URL) {
		return ErrSeekBrowserUnsupported
	}
	return nil
}
