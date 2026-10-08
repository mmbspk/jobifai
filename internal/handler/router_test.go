package handler_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/handler"
)

// buildSPATestRouter creates a router with a minimal fake web/dist directory so the SPA
// fallback is active. Returns the router and the temp dir path.
func buildSPATestRouter(t *testing.T) (http.Handler, string) {
	t.Helper()
	webDist := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(webDist, "index.html"), []byte("<html>SPA</html>"), 0o644))
	t.Setenv("WEB_DIST", webDist)
	svc, _ := newTestServices(t)
	return handler.NewRouter(svc), webDist
}

// TestVerifyEmailRoute_ServedBySPA checks that GET /verify-email falls through to the
// SPA fallback and does NOT match the /auth/verify-email API endpoint.
func TestVerifyEmailRoute_ServedBySPA(t *testing.T) {
	router, _ := buildSPATestRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/verify-email?token=testtoken", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "SPA", "expected SPA index.html to be served for /verify-email")
}

// TestVerifyEmailChangeRoute_ServedBySPA checks that GET /verify-email-change falls
// through to the SPA fallback and does NOT match the /auth/verify-email-change API endpoint.
func TestVerifyEmailChangeRoute_ServedBySPA(t *testing.T) {
	router, _ := buildSPATestRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/verify-email-change?token=testtoken", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "SPA", "expected SPA index.html to be served for /verify-email-change")
}

// TestAuthVerifyEmailRoute_StillAPIEndpoint checks that GET /auth/verify-email still
// resolves as a backend API endpoint (not served by SPA).
func TestAuthVerifyEmailRoute_StillAPIEndpoint(t *testing.T) {
	router, _ := buildSPATestRouter(t)
	// /auth/verify-email with no token should return JSON, not the SPA
	req := httptest.NewRequest(http.MethodGet, "/auth/verify-email?token=bogus", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	// The API returns 4xx on bogus/missing token; NOT the SPA HTML
	assert.NotEqual(t, http.StatusOK, rr.Code, "expected /auth/verify-email to be an API endpoint, not SPA")
	assert.NotContains(t, rr.Body.String(), "SPA", "/auth/verify-email must not be served as SPA")
}

// TestAuthVerifyEmailChangeRoute_StillAPIEndpoint checks that GET /auth/verify-email-change
// still resolves as a backend API endpoint (not served by SPA).
func TestAuthVerifyEmailChangeRoute_StillAPIEndpoint(t *testing.T) {
	router, _ := buildSPATestRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/auth/verify-email-change?token=bogus", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.NotEqual(t, http.StatusOK, rr.Code, "expected /auth/verify-email-change to be an API endpoint, not SPA")
	assert.NotContains(t, rr.Body.String(), "SPA", "/auth/verify-email-change must not be served as SPA")
}
