// Package browser manages visible Chrome windows for manual job-site login
// and captures the resulting cookies for reuse by the automation bot.
package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/google/uuid"
	"github.com/user/jobifai/internal/domain"
)

// PlatformURL is the login page opened for each platform.
var PlatformURL = map[string]string{
	"linkedin": "https://www.linkedin.com/login",
	"seek":     "https://au.seek.com",
}

// Cookie is a serialisable browser cookie.
type Cookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path"`
	Expires  float64 `json:"expires"`
	HTTPOnly bool    `json:"http_only"`
	Secure   bool    `json:"secure"`
	SameSite string  `json:"same_site"`
}

// Session represents an open browser window awaiting cookie capture.
type Session struct {
	ID        string
	Platform  string
	UserID    string
	Browser   *rod.Browser
	CreatedAt time.Time
}

// Manager keeps track of open browser sessions (one per API call).
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*Session
}

func NewManager() *Manager {
	return &Manager{sessions: make(map[string]*Session)}
}

// ErrAlreadyOpen is returned when a session for the same platform is already open.
var ErrAlreadyOpen = fmt.Errorf("browser: a session is already open for this platform: %w", domain.ErrAlreadyOpen)

// ErrNotFound is returned when the session ID is unknown.
var ErrNotFound = fmt.Errorf("browser: session not found: %w", domain.ErrNotFound)

// ErrSessionOwnership is returned when a session belongs to a different user.
var ErrSessionOwnership = fmt.Errorf("browser: session belongs to a different user: %w", domain.ErrSessionOwnership)

// ErrNotLoggedIn is returned when CaptureCookies runs but the page is still logged out.
var ErrNotLoggedIn = fmt.Errorf("browser: not logged in: %w", domain.ErrNotLoggedIn)

// ProfileDir returns the Chrome user-data-dir to use for a platform session.
// When configured is non-empty it wins; otherwise a stable per-user path under
// data/chrome-profiles/ is used so Auth0/localStorage survives across launches.
func ProfileDir(userID, platform, configured string) string {
	if configured != "" {
		return configured
	}
	return filepath.Join("data", "chrome-profiles", userID, platform)
}

// chromeLockPID reads data/chrome-profiles/.../SingletonLock (a symlink whose
// target ends in -<pid>) and returns that pid, or 0 if unreadable.
func chromeLockPID(dir string) int {
	target, err := os.Readlink(filepath.Join(dir, "SingletonLock"))
	if err != nil {
		return 0
	}
	i := strings.LastIndex(target, "-")
	if i < 0 || i+1 >= len(target) {
		return 0
	}
	pid, err := strconv.Atoi(target[i+1:])
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}

func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

func removeChromeSingletonFiles(dir string) {
	for _, name := range []string{"SingletonLock", "SingletonSocket", "SingletonCookie"} {
		_ = os.Remove(filepath.Join(dir, name))
	}
}

// ClearStaleChromeProfileLocks removes SingletonLock/Socket/Cookie when the
// locking Chrome process is no longer running. Safe no-op if the lock is live.
func ClearStaleChromeProfileLocks(dir string) {
	if dir == "" {
		return
	}
	pid := chromeLockPID(dir)
	if pid == 0 {
		return
	}
	if processAlive(pid) {
		return
	}
	removeChromeSingletonFiles(dir)
}

// ForceUnlockChromeProfile kills a live Chrome still holding jobifai's profile
// lock (dedicated data/chrome-profiles path only) then removes lock files.
// Use before a retry when Launch fails with SingletonLock "File exists".
func ForceUnlockChromeProfile(dir string) {
	if dir == "" {
		return
	}
	if pid := chromeLockPID(dir); pid > 0 && processAlive(pid) {
		if p, err := os.FindProcess(pid); err == nil {
			_ = p.Kill()
			time.Sleep(500 * time.Millisecond)
		}
	}
	removeChromeSingletonFiles(dir)
}

// PrepareChromeProfileDir ensures the profile directory exists and clears a
// stale SingletonLock so a new Chrome can launch after a crash / abrupt stop.
func PrepareChromeProfileDir(dir string) {
	if dir == "" {
		return
	}
	_ = os.MkdirAll(dir, 0o750)
	ClearStaleChromeProfileLocks(dir)
}

// ReleaseProfileForLaunch stops any Chrome still holding dir and clears singleton
// files so a fresh visible window can open (Settings → Connect browser).
func ReleaseProfileForLaunch(dir string) {
	ForceUnlockChromeProfile(dir)
	time.Sleep(400 * time.Millisecond)
}

// Launch opens a visible (non-headless) Chrome window navigated to the
// platform's login page. Returns the session ID the caller must pass to
// CaptureCookies later.
//
// When useProfile is true, Chrome uses a persistent user-data-dir (profilePath
// or ProfileDir default) so Auth0 SPA state in localStorage is preserved.
func (m *Manager) Launch(userID, platform, profilePath string, useProfile bool) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Enforce one open session per platform
	for _, s := range m.sessions {
		if s.Platform == platform {
			return "", ErrAlreadyOpen
		}
	}

	loginURL, ok := PlatformURL[platform]
	if !ok {
		return "", fmt.Errorf("browser: unknown platform %q", platform)
	}

	l := launcher.New().
		Headless(false).
		Set("--disable-blink-features", "AutomationControlled").
		Set("--no-sandbox").
		Set("--disable-gpu").
		Set("--disable-dev-shm-usage")

	profileDir := ""
	if useProfile {
		profileDir = ProfileDir(userID, platform, profilePath)
		PrepareChromeProfileDir(profileDir)
		l = l.UserDataDir(profileDir)
	}

	url, err := l.Launch()
	if err != nil && profileDir != "" && strings.Contains(err.Error(), "SingletonLock") {
		ForceUnlockChromeProfile(profileDir)
		time.Sleep(400 * time.Millisecond)
		url, err = launcher.New().
			Headless(false).
			Set("--disable-blink-features", "AutomationControlled").
			Set("--no-sandbox").
			Set("--disable-gpu").
			Set("--disable-dev-shm-usage").
			UserDataDir(profileDir).
			Launch()
	}
	if err != nil {
		return "", fmt.Errorf("browser: launch chrome: %w", err)
	}

	b := rod.New().ControlURL(url).MustConnect()

	page := b.MustPage(loginURL)
	_ = page // user interacts manually

	sess := &Session{
		ID:        uuid.New().String(),
		Platform:  platform,
		UserID:    userID,
		Browser:   b,
		CreatedAt: time.Now(),
	}
	m.sessions[sess.ID] = sess
	return sess.ID, nil
}

// CaptureCookies extracts all cookies from the open browser associated with
// sessionID, closes that browser, and returns the JSON-encoded cookies.
// Returns ErrSessionOwnership if the session belongs to a different user.
// Returns ErrNotLoggedIn if the page still looks logged out — callers must
// finish login before saving, otherwise a dead session would be persisted.
func (m *Manager) CaptureCookies(ctx context.Context, userID, sessionID string) ([]byte, error) {
	m.mu.Lock()
	sess, ok := m.sessions[sessionID]
	if !ok {
		m.mu.Unlock()
		return nil, ErrNotFound
	}
	if sess.UserID != userID {
		m.mu.Unlock()
		return nil, ErrSessionOwnership
	}
	delete(m.sessions, sessionID)
	m.mu.Unlock()

	defer func() {
		// Best-effort close; ignore errors
		_ = sess.Browser.Close()
	}()

	pages, err := sess.Browser.Pages()
	if err != nil || len(pages) == 0 {
		return nil, fmt.Errorf("browser: no pages open")
	}
	page := pages[0]

	// Prefer a page on the platform host that is not a login URL.
	for _, p := range pages {
		info, ierr := p.Info()
		if ierr != nil || !platformHostMatch(sess.Platform, info.URL) {
			continue
		}
		page = p
		u := strings.ToLower(info.URL)
		if sess.Platform == "seek" && (strings.Contains(u, "login.seek.com") || strings.Contains(u, "/oauth/login")) {
			continue
		}
		if sess.Platform == "linkedin" && (strings.Contains(u, "/login") || strings.Contains(u, "/checkpoint")) {
			continue
		}
		break
	}

	_ = page.Timeout(5 * time.Second).WaitStable(500 * time.Millisecond)
	state, stateErr := PageLoginState(page, sess.Platform)
	// Only block when we positively see a logged-out UI. Seek's DOM often
	// returns "unknown" even when signed in (selectors drift); requiring "yes"
	// broke Save session for valid logins.
	if stateErr == nil && state == LoginStateNo {
		return nil, fmt.Errorf("%w: finish logging in to %s in the browser window, then save again",
			ErrNotLoggedIn, sess.Platform)
	}

	// NetworkGetAllCookies returns every cookie in the browser regardless of domain,
	// including subdomains (id.seek.com, api.seek.com, etc.) that page.Cookies(nil)
	// would miss since that only returns cookies matching the current page URL.
	result, err := proto.NetworkGetAllCookies{}.Call(page)
	if err != nil {
		return nil, fmt.Errorf("browser: get all cookies: %w", err)
	}

	var out []Cookie
	for _, c := range result.Cookies {
		out = append(out, Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   string(c.Domain),
			Path:     c.Path,
			Expires:  float64(c.Expires),
			HTTPOnly: c.HTTPOnly,
			Secure:   bool(c.Secure),
			SameSite: string(c.SameSite),
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: no cookies captured — finish logging in first", ErrNotLoggedIn)
	}
	// Extra guard for Seek: require at least one cookie on a seek.com host so we
	// don't persist an empty/guest jar after a failed login.
	if sess.Platform == "seek" && !hasSeekHostCookie(out) {
		return nil, fmt.Errorf("%w: no Seek cookies found — finish signing in, then save again", ErrNotLoggedIn)
	}
	return MarshalCookies(out)
}

// LoginState is the result of probing whether a platform page looks authenticated.
type LoginState string

const (
	LoginStateYes     LoginState = "yes"
	LoginStateNo      LoginState = "no"
	LoginStateUnknown LoginState = "unknown"
)

// PageLoginState probes the DOM/URL for signed-in indicators on a platform page.
func PageLoginState(page *rod.Page, platform string) (LoginState, error) {
	script := loginStateScript(platform)
	if script == "" {
		return LoginStateUnknown, nil
	}
	res, err := page.Eval(script)
	if err != nil {
		return LoginStateUnknown, err
	}
	switch res.Value.Str() {
	case "yes":
		return LoginStateYes, nil
	case "no":
		return LoginStateNo, nil
	default:
		return LoginStateUnknown, nil
	}
}

// ShouldPersistCookies reports whether a cookie snapshot should be written back
// to the session store. Only persist when we positively see a logged-in UI —
// never overwrite a good session with a guest/unknown jar.
func ShouldPersistCookies(state LoginState, checkErr error) bool {
	if checkErr != nil {
		return false
	}
	return state == LoginStateYes
}

func loginStateScript(platform string) string {
	switch platform {
	case "seek":
		// Seek keeps /oauth/login links in the DOM even when signed in — never
		// treat their mere presence as logged-out. Prefer explicit sign-out /
		// account markers from the live header.
		return `() => {
			const u = (location.href || '').toLowerCase();
			if (u.includes('login.seek.com') || u.includes('/oauth/login') || /\/sign-?in\b/.test(u)) return 'no';
			const q = (s) => document.querySelector(s);
			if (q('[data-automation="sign out"], [data-automation="sign-out"], [data-automation="account name"], [data-automation="mobile-profile-avatar-wrapper"]')) return 'yes';
			if (q('[data-automation="account-nav"], [data-automation="signed-in-nav"], [aria-label="My account"]')) return 'yes';
			if (q('a[href="/dashboard"], [data-automation="profile-link"], [data-automation="profile"]')) return 'yes';
			// Visible primary sign-in CTA (not buried footer links).
			const signIn = q('[data-automation="sign in"], [data-automation="sign-in"], [data-automation="sign-in-register"]');
			if (signIn) {
				const style = window.getComputedStyle(signIn);
				if (style && style.display !== 'none' && style.visibility !== 'hidden') return 'no';
			}
			return 'unknown';
		}`
	case "linkedin":
		return `() => {
			const u = (location.href || '').toLowerCase();
			if (u.includes('/login') || u.includes('/checkpoint') || u.includes('/authwall') || u.includes('/uas/login')) return 'no';
			if (document.querySelector('input[name="session_key"], form.login__form, .login__form')) return 'no';
			if (document.querySelector('#global-nav, .global-nav, [data-test-global-nav], .feed-identity-module')) return 'yes';
			if (document.querySelector('.global-nav__me, img.global-nav__me-photo')) return 'yes';
			return 'unknown';
		}`
	default:
		return ""
	}
}

func platformHostMatch(platform, pageURL string) bool {
	u := strings.ToLower(pageURL)
	switch platform {
	case "seek":
		return strings.Contains(u, "seek.com")
	case "linkedin":
		return strings.Contains(u, "linkedin.com")
	default:
		return false
	}
}

func hasSeekHostCookie(cookies []Cookie) bool {
	for _, c := range cookies {
		d := strings.ToLower(c.Domain)
		if strings.Contains(d, "seek.com") {
			return true
		}
	}
	return false
}

// MarshalCookies serialises cookies to JSON bytes ready for encryption.
func MarshalCookies(cookies []Cookie) ([]byte, error) {
	return json.Marshal(cookies)
}

// UnmarshalCookies deserialises cookies from JSON bytes.
func UnmarshalCookies(data []byte) ([]Cookie, error) {
	var cookies []Cookie
	if err := json.Unmarshal(data, &cookies); err != nil {
		return nil, err
	}
	return cookies, nil
}

// ToCookieParams converts our Cookie type to go-rod's SetCookiesParams so
// the bot can inject them into a new browser session.
// Seek migrated from seek.com.au to seek.com — rewrite legacy domains so
// old saved sessions still work on the new domain.
func ToCookieParams(cookies []Cookie) []*proto.NetworkCookieParam {
	out := make([]*proto.NetworkCookieParam, 0, len(cookies))
	for i := range cookies {
		c := &cookies[i]
		domain := c.Domain
		if strings.Contains(domain, "seek.com.au") {
			domain = strings.ReplaceAll(domain, "seek.com.au", "seek.com")
		}
		p := &proto.NetworkCookieParam{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   domain,
			Path:     c.Path,
			HTTPOnly: c.HTTPOnly,
			Secure:   c.Secure,
			Expires:  proto.TimeSinceEpoch(c.Expires),
			SameSite: proto.NetworkCookieSameSite(c.SameSite),
		}
		out = append(out, p)
	}
	return out
}