package auth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/user/jobifai/internal/auth"
	appdb "github.com/user/jobifai/internal/db"
)

func TestGoogle_Redirect_SetsStateCookie(t *testing.T) {
	db, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	tm := auth.NewTokenManager("test-jwt-secret-for-google-oauth")
	h := auth.NewGoogleHandler(auth.GoogleConfig{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		RedirectURL:  "http://localhost/auth/google/callback",
	}, db, tm, func(context.Context, string, string, string, string) (string, error) {
		return "user-1", nil
	})

	req := httptest.NewRequest(http.MethodGet, "/auth/google", nil)
	rec := httptest.NewRecorder()
	h.Redirect(rec, req)

	assert.Equal(t, http.StatusFound, rec.Code)
	assert.NotEmpty(t, rec.Header().Get("Location"))

	cookies := rec.Result().Cookies()
	var stateCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "oauth_state" {
			stateCookie = c
			break
		}
	}
	require.NotNil(t, stateCookie)
	assert.NotEmpty(t, stateCookie.Value)
	assert.True(t, stateCookie.HttpOnly)
}

func TestGoogle_Callback_InvalidState(t *testing.T) {
	db, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	tm := auth.NewTokenManager("test-jwt-secret-for-google-oauth")
	h := auth.NewGoogleHandler(auth.GoogleConfig{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		RedirectURL:  "http://localhost/auth/google/callback",
	}, db, tm, nil)

	req := httptest.NewRequest(http.MethodGet, "/auth/google/callback?state=wrong&code=abc", nil)
	req.AddCookie(&http.Cookie{Name: "oauth_state", Value: "expected"})
	rec := httptest.NewRecorder()
	h.Callback(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestGoogle_Callback_Success_IssuesAppTokens(t *testing.T) {
	userInfo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"sub":     "google-sub-123",
			"email":   "oauth-user@example.com",
			"name":    "OAuth User",
			"picture": "https://example.com/avatar.png",
		})
	}))
	t.Cleanup(userInfo.Close)

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "mock-google-access",
			"token_type":    "Bearer",
			"expires_in":    3600,
			"refresh_token": "mock-google-refresh",
		})
	}))
	t.Cleanup(tokenSrv.Close)

	db, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	tm := auth.NewTokenManager("test-jwt-secret-for-google-oauth")
	h := auth.NewGoogleHandler(auth.GoogleConfig{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		RedirectURL:  "http://localhost/auth/google/callback",
		Endpoint: oauth2.Endpoint{
			AuthURL:  tokenSrv.URL + "/auth",
			TokenURL: tokenSrv.URL + "/token",
		},
		UserInfoURL: userInfo.URL,
	}, db, tm, func(_ context.Context, googleID, email, name, avatar string) (string, error) {
		assert.Equal(t, "google-sub-123", googleID)
		assert.Equal(t, "oauth-user@example.com", email)
		assert.Equal(t, "OAuth User", name)
		assert.NotEmpty(t, avatar)
		return "app-user-oauth-1", nil
	})

	state := "csrf-state-token"
	req := httptest.NewRequest(http.MethodGet, "/auth/google/callback?code=auth-code&state="+state, nil)
	req.AddCookie(&http.Cookie{Name: "oauth_state", Value: state})
	rec := httptest.NewRecorder()
	h.Callback(rec, req)

	require.Equal(t, http.StatusFound, rec.Code)
	loc := rec.Header().Get("Location")
	assert.True(t, strings.HasPrefix(loc, "/#access_token="))

	fragment := strings.TrimPrefix(loc, "/#")
	vals, err := url.ParseQuery(fragment)
	require.NoError(t, err)
	require.NotEmpty(t, vals.Get("access_token"))
	require.NotEmpty(t, vals.Get("refresh_token"))

	claims, err := tm.Verify(vals.Get("access_token"))
	require.NoError(t, err)
	assert.Equal(t, "app-user-oauth-1", claims.UserID)
	assert.Equal(t, "oauth-user@example.com", claims.Email)
}
