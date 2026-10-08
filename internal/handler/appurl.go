package handler

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/user/jobifai/internal/domain"
)

const keyAppSettings = "app_settings"

// NormalizeAndValidateURL validates and normalizes a public app URL.
// Returns ("", nil) for empty input (clearing the value is allowed).
// Rejects: non-http/https schemes, missing host, query strings, fragments.
// Normalizes: scheme+host only, strips trailing slash and any path.
func NormalizeAndValidateURL(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("URL must use http or https scheme, got %q", u.Scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("URL must include a host")
	}
	if u.RawQuery != "" {
		return "", fmt.Errorf("URL must not include a query string")
	}
	if u.Fragment != "" {
		return "", fmt.Errorf("URL must not include a fragment")
	}
	return u.Scheme + "://" + u.Host, nil
}

// isLoopbackOrigin returns true only for http/https Origins whose host is
// localhost, 127.0.0.1, or ::1. All other hosts are rejected to prevent
// arbitrary Origin trust from leaking into production email links.
func isLoopbackOrigin(origin string) bool {
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	h := u.Hostname()
	return h == "localhost" || h == "127.0.0.1" || h == "::1"
}

// ResolvePublicAppURL returns the canonical public base URL using this priority:
//
//	A. Persisted Admin Defaults public_app_url (ConfigStore, __system__ / app_settings)
//	B. Services.AppBaseURL (APP_BASE_URL env var, set at startup)
//	C. Loopback-only validated Origin from the HTTP request (localhost / 127.0.0.1 / ::1)
//	D. http://localhost:8081 final fallback (logs a warning)
//
// Call this BEFORE spawning goroutines so the resolved URL can be passed as a
// plain string. Never pass *http.Request into goroutines.
func ResolvePublicAppURL(svc *Services, r *http.Request) string {
	// A: persisted admin setting
	if svc.Config != nil {
		var appCfg domain.ApplicationSettings
		_ = svc.Config.Get(domain.SystemUserID, keyAppSettings, &appCfg)
		if appCfg.PublicAppURL != "" {
			return appCfg.PublicAppURL
		}
	}
	// B: APP_BASE_URL env var
	if svc.AppBaseURL != "" {
		return svc.AppBaseURL
	}
	// C: loopback Origin from the current request
	if r != nil {
		if origin := r.Header.Get("Origin"); isLoopbackOrigin(origin) {
			if u, err := url.Parse(origin); err == nil {
				return u.Scheme + "://" + u.Host
			}
		}
	}
	// D: fallback for local dev
	log.Warn().Msg("public app URL not configured — email links use http://localhost:8081 fallback; set APP_BASE_URL or configure Admin → Defaults → Application")
	return "http://localhost:8081"
}

// buildVerifyURL constructs the email-verification link from an already-resolved base URL.
func buildVerifyURL(base, token string) string {
	return fmt.Sprintf("%s/auth/verify-email?token=%s", strings.TrimRight(base, "/"), token)
}

// buildPasswordResetURL constructs the password-reset link from an already-resolved base URL.
func buildPasswordResetURL(base, token string) string {
	return fmt.Sprintf("%s/reset-password?token=%s", strings.TrimRight(base, "/"), token)
}

// buildEmailChangeVerifyURL constructs the email-change verification link from an already-resolved base URL.
func buildEmailChangeVerifyURL(base, token string) string {
	return fmt.Sprintf("%s/auth/verify-email-change?token=%s", strings.TrimRight(base, "/"), token)
}
