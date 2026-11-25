package browser

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/go-rod/rod"
)

// ErrSeekBrowserUnsupported must not be treated as a successful empty search
// or an authentication failure that can be fixed by repeatedly signing in.
var ErrSeekBrowserUnsupported = errors.New("SEEK's browser compatibility page could not be cleared. Open the SEEK session browser and use Continue and remind me later, or update Chrome/Chromium, then reconnect the session")

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

// RecoverSeekBrowserUpgrade uses SEEK's own compatibility acknowledgement.
// It preserves the real installed browser identity and returns to the caller's
// intended URL once. A remaining upgrade page must never reach job evaluation.
func RecoverSeekBrowserUpgrade(page *rod.Page, intendedURL string) error {
	if err := CheckSeekBrowser(page); !errors.Is(err, ErrSeekBrowserUnsupported) {
		return err
	}
	target, err := url.Parse(intendedURL)
	if err != nil || target.Scheme != "https" || target.User != nil || !seekHost(target.Hostname()) {
		return fmt.Errorf("%w: intended SEEK page is invalid", ErrSeekBrowserUnsupported)
	}
	info, err := page.Info()
	if err != nil {
		return err
	}
	notice, err := url.Parse(info.URL)
	if err != nil {
		return err
	}
	if next := notice.Query().Get("continue"); next != "" {
		nextURL, parseErr := url.Parse(next)
		if parseErr != nil {
			return ErrSeekBrowserUnsupported
		}
		nextURL = notice.ResolveReference(nextURL)
		if nextURL.Scheme != "https" || nextURL.User != nil || !strings.EqualFold(nextURL.Hostname(), target.Hostname()) {
			return fmt.Errorf("%w: compatibility notice points outside the intended SEEK site", ErrSeekBrowserUnsupported)
		}
	}
	page = page.Timeout(12 * time.Second)
	button, err := page.ElementByJS(rod.Eval(`() => {
		const visible = el => { const r = el.getBoundingClientRect(), s = getComputedStyle(el); return r.width > 0 && r.height > 0 && s.display !== 'none' && s.visibility !== 'hidden'; };
		const buttons = Array.from(document.querySelectorAll('button,[role="button"]')).filter(el =>
			visible(el) && !el.disabled && el.getAttribute('aria-disabled') !== 'true' &&
			/^Continue and remind me later$/i.test((el.textContent || '').replace(/\s+/g, ' ').trim()));
		return buttons.length === 1 ? buttons[0] : null;
	}`))
	if err != nil {
		return fmt.Errorf("%w: SEEK's continue option is unavailable", ErrSeekBrowserUnsupported)
	}
	if err := button.Click("left", 1); err != nil {
		return fmt.Errorf("%w: could not acknowledge SEEK's compatibility notice", ErrSeekBrowserUnsupported)
	}
	_ = page.WaitLoad()
	// The notice may send users to the home page if its continue query is absent.
	// Always restore the declared destination, never scrape that fallback page.
	if err := page.Navigate(intendedURL); err != nil {
		return err
	}
	_ = page.WaitLoad()
	_ = page.WaitStable(500 * time.Millisecond)
	return CheckSeekBrowser(page)
}

func seekHost(host string) bool {
	host = strings.ToLower(host)
	return host == "seek.com" || strings.HasSuffix(host, ".seek.com") || host == "seek.com.au" || strings.HasSuffix(host, ".seek.com.au")
}
