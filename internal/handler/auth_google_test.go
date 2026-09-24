package handler_test

// Router-level Google OAuth tests (handler wiring). OAuth logic unit tests live in internal/auth/google_test.go.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/user/jobifai/internal/auth"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/handler"
)

func TestAuth_GoogleOAuth_DisabledWhenNotConfigured(t *testing.T) {
	svc, _ := newTestServices(t)
	svc.Google = nil
	router := handler.NewRouter(svc)

	req := httptest.NewRequest(http.MethodGet, "/auth/google", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestAuth_GoogleRedirect_WhenConfigured(t *testing.T) {
	db, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	svc, _ := newTestServices(t)
	svc.DB = db
	svc.Google = auth.NewGoogleHandler(auth.GoogleConfig{
		ClientID:     "client",
		ClientSecret: "secret",
		RedirectURL:  "http://127.0.0.1/auth/google/callback",
	}, db, svc.TokenManager, func(context.Context, string, string, string, string) (string, error) {
		return "uid", nil
	})

	router := handler.NewRouter(svc)
	req := httptest.NewRequest(http.MethodGet, "/auth/google", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusFound, rec.Code)
}

func TestAuth_GoogleCallback_MockOAuthServer(t *testing.T) {
	userInfo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"sub": "g-1", "email": "g@example.com", "name": "G User",
		})
	}))
	t.Cleanup(userInfo.Close)

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "tok",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	t.Cleanup(tokenSrv.Close)

	db, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	svc, _ := newTestServices(t)
	svc.DB = db
	svc.Google = auth.NewGoogleHandler(auth.GoogleConfig{
		ClientID:     "client",
		ClientSecret: "secret",
		RedirectURL:  "http://127.0.0.1/auth/google/callback",
		Endpoint: oauth2.Endpoint{
			AuthURL:  tokenSrv.URL + "/auth",
			TokenURL: tokenSrv.URL + "/token",
		},
		UserInfoURL: userInfo.URL,
	}, db, svc.TokenManager, func(_ context.Context, _, email, _, _ string) (string, error) {
		hash, err := auth.HashPassword("oauth-pass-123456")
		if err != nil {
			return "", err
		}
		u, err := svc.Users.Create(email, hash, "Google User")
		if err != nil {
			return "", err
		}
		return u.ID, nil
	})

	router := handler.NewRouter(svc)
	state := "state-xyz"
	req := httptest.NewRequest(http.MethodGet, "/auth/google/callback?code=c1&state="+state, nil)
	req.AddCookie(&http.Cookie{Name: "oauth_state", Value: state})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusFound, rec.Code)
}
