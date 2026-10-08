package handler_test

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/handler"
)

// ─── NormalizeAndValidateURL ───────────────────────────────────────────────

func TestNormalizeAndValidateURL_Empty(t *testing.T) {
	got, err := handler.NormalizeAndValidateURL("")
	require.NoError(t, err)
	assert.Equal(t, "", got)
}

func TestNormalizeAndValidateURL_TrailingSlash(t *testing.T) {
	got, err := handler.NormalizeAndValidateURL("https://example.com/")
	require.NoError(t, err)
	assert.Equal(t, "https://example.com", got)
}

func TestNormalizeAndValidateURL_PathStripped(t *testing.T) {
	got, err := handler.NormalizeAndValidateURL("https://example.com/some/path")
	require.NoError(t, err)
	assert.Equal(t, "https://example.com", got)
}

func TestNormalizeAndValidateURL_RejectsQueryString(t *testing.T) {
	_, err := handler.NormalizeAndValidateURL("https://x.com/path?q=1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "query string")
}

func TestNormalizeAndValidateURL_RejectsFragment(t *testing.T) {
	_, err := handler.NormalizeAndValidateURL("https://x.com#section")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fragment")
}

func TestNormalizeAndValidateURL_RejectsNonHTTPScheme(t *testing.T) {
	_, err := handler.NormalizeAndValidateURL("ftp://x.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "http")
}

func TestNormalizeAndValidateURL_HTTP(t *testing.T) {
	got, err := handler.NormalizeAndValidateURL("http://localhost:8081")
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:8081", got)
}

// ─── ResolvePublicAppURL ───────────────────────────────────────────────────

func TestResolvePublicAppURL_PersistedURLWins(t *testing.T) {
	svc, db := newTestServices(t)
	_ = db
	cfg := config.NewStore(db)
	svc.Config = cfg
	svc.AppBaseURL = "https://env-url.example"

	err := cfg.Set(domain.SystemUserID, "app_settings", domain.ApplicationSettings{PublicAppURL: "https://persisted.example"})
	require.NoError(t, err)

	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Origin", "http://localhost:5173")

	got := handler.ResolvePublicAppURL(svc, r)
	assert.Equal(t, "https://persisted.example", got)
}

func TestResolvePublicAppURL_AppBaseURLFallback(t *testing.T) {
	svc, _ := newTestServices(t)
	svc.AppBaseURL = "https://env-url.example"

	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Origin", "http://localhost:5173")

	got := handler.ResolvePublicAppURL(svc, r)
	assert.Equal(t, "https://env-url.example", got)
}

func TestResolvePublicAppURL_LoopbackOriginFallback(t *testing.T) {
	svc, _ := newTestServices(t)
	svc.AppBaseURL = ""

	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Origin", "http://localhost:5173")

	got := handler.ResolvePublicAppURL(svc, r)
	assert.Equal(t, "http://localhost:5173", got)
}

func TestResolvePublicAppURL_MaliciousOriginRejected(t *testing.T) {
	svc, _ := newTestServices(t)
	svc.AppBaseURL = ""

	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Origin", "http://evil.com")

	got := handler.ResolvePublicAppURL(svc, r)
	assert.Equal(t, "http://localhost:8081", got)
}

func TestResolvePublicAppURL_NoRequestFallback(t *testing.T) {
	svc, _ := newTestServices(t)
	svc.AppBaseURL = ""

	got := handler.ResolvePublicAppURL(svc, nil)
	assert.Equal(t, "http://localhost:8081", got)
}

// ─── BootstrapPublicAppURL ─────────────────────────────────────────────────

func TestBootstrapPublicAppURL_SeedsMissingValue(t *testing.T) {
	db := newTestDB(t)
	cfg := config.NewStore(db)

	handler.BootstrapPublicAppURL(cfg, "https://boot.example")

	var got domain.ApplicationSettings
	require.NoError(t, cfg.Get(domain.SystemUserID, "app_settings", &got))
	assert.Equal(t, "https://boot.example", got.PublicAppURL)
}

func TestBootstrapPublicAppURL_DoesNotOverwrite(t *testing.T) {
	db := newTestDB(t)
	cfg := config.NewStore(db)
	require.NoError(t, cfg.Set(domain.SystemUserID, "app_settings", domain.ApplicationSettings{PublicAppURL: "https://existing.example"}))

	handler.BootstrapPublicAppURL(cfg, "https://different.example")

	var got domain.ApplicationSettings
	require.NoError(t, cfg.Get(domain.SystemUserID, "app_settings", &got))
	assert.Equal(t, "https://existing.example", got.PublicAppURL)
}

func TestBootstrapPublicAppURL_NoopWhenEnvEmpty(t *testing.T) {
	db := newTestDB(t)
	cfg := config.NewStore(db)

	handler.BootstrapPublicAppURL(cfg, "")

	var got domain.ApplicationSettings
	_ = cfg.Get(domain.SystemUserID, "app_settings", &got)
	assert.Equal(t, "", got.PublicAppURL)
}
