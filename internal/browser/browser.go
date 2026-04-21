// Package browser manages visible Chrome windows for manual job-site login
// and captures the resulting cookies for reuse by the automation bot.
package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/google/uuid"
)

// PlatformURL is the login page opened for each platform.
var PlatformURL = map[string]string{
	"linkedin": "https://www.linkedin.com/login",
	"seek":     "https://www.seek.com.au/login",
	"indeed":   "https://secure.indeed.com/account/login",
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
}

// Session represents an open browser window awaiting cookie capture.
type Session struct {
	ID          string
	Platform    string
	Browser     *rod.Browser
	CreatedAt   time.Time
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
var ErrAlreadyOpen = errors.New("browser: a session is already open for this platform")

// ErrNotFound is returned when the session ID is unknown.
var ErrNotFound = errors.New("browser: session not found")

// Launch opens a visible (non-headless) Chrome window navigated to the
// platform's login page. Returns a session ID the caller must pass to
// CaptureCookies later.
func (m *Manager) Launch(platform, profilePath string, useProfile bool) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Enforce one open session per platform
	for _, s := range m.sessions {
		if s.Platform == platform {
			return nil, ErrAlreadyOpen
		}
	}

	loginURL, ok := PlatformURL[platform]
	if !ok {
		return nil, fmt.Errorf("browser: unknown platform %q", platform)
	}

	l := launcher.New().
		Headless(false).              // visible window
		Set("--disable-blink-features", "AutomationControlled").
		Set("--no-sandbox")

	if useProfile && profilePath != "" {
		l = l.UserDataDir(profilePath)
	}

	url, err := l.Launch()
	if err != nil {
		return nil, fmt.Errorf("browser: launch chrome: %w", err)
	}

	b := rod.New().ControlURL(url).MustConnect()

	page := b.MustPage(loginURL)
	_ = page // user interacts manually

	sess := &Session{
		ID:        uuid.New().String(),
		Platform:  platform,
		Browser:   b,
		CreatedAt: time.Now(),
	}
	m.sessions[sess.ID] = sess
	return sess, nil
}

// CaptureCookies extracts all cookies from the open browser associated with
// sessionID, closes that browser, and returns the cookies.
func (m *Manager) CaptureCookies(ctx context.Context, sessionID string) ([]Cookie, error) {
	m.mu.Lock()
	sess, ok := m.sessions[sessionID]
	if !ok {
		m.mu.Unlock()
		return nil, ErrNotFound
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

	// Collect cookies from all open pages
	seen := map[string]bool{}
	var out []Cookie

	for _, page := range pages {
		raw, err := page.Cookies(nil)
		if err != nil {
			continue
		}
		for _, c := range raw {
			key := c.Name + "|" + c.Domain
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, Cookie{
				Name:     c.Name,
				Value:    c.Value,
				Domain:   string(c.Domain),
				Path:     c.Path,
				Expires:  float64(c.Expires),
				HTTPOnly: c.HTTPOnly,
				Secure:   bool(c.Secure),
			})
		}
	}
	return out, nil
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
func ToCookieParams(cookies []Cookie) []*proto.NetworkCookieParam {
	out := make([]*proto.NetworkCookieParam, 0, len(cookies))
	for i := range cookies {
		c := &cookies[i]
		out = append(out, &proto.NetworkCookieParam{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			HTTPOnly: c.HTTPOnly,
			Secure:   c.Secure,
		})
	}
	return out
}
