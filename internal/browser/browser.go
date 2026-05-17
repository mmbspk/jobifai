// Package browser manages visible Chrome windows for manual job-site login
// and captures the resulting cookies for reuse by the automation bot.
package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
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

// Launch opens a visible (non-headless) Chrome window navigated to the
// platform's login page. Returns the session ID the caller must pass to
// CaptureCookies later.
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

	if useProfile && profilePath != "" {
		l = l.UserDataDir(profilePath)
	}

	url, err := l.Launch()
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

	// NetworkGetAllCookies returns every cookie in the browser regardless of domain,
	// including subdomains (id.seek.com, api.seek.com, etc.) that page.Cookies(nil)
	// would miss since that only returns cookies matching the current page URL.
	result, err := proto.NetworkGetAllCookies{}.Call(pages[0])
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
	return MarshalCookies(out)
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