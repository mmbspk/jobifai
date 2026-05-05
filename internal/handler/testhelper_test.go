package handler_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/config"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/handler"
	"github.com/user/jobifai/internal/ws"
)

// newTestDB opens a fresh SQLite database with all migrations applied.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

// newTestServices builds a minimal Services bag backed by real SQLite.
// Fields that are not needed by the test (LLM, browser, bot) are left nil.
func newTestServices(t *testing.T) (*handler.Services, *sql.DB) {
	t.Helper()
	db := newTestDB(t)
	tm := auth.NewTokenManager("test-jwt-secret")
	return &handler.Services{
		DB:           db,
		Config:       config.NewStore(db),
		Secrets:      config.NewSecretsStore(db, "test-passphrase"),
		Users:        auth.NewUserStore(db),
		TokenManager: tm,
		Logs:         ws.NewBroadcaster(),
	}, db
}

// registerAndLogin creates a user and returns a valid Bearer access token.
func registerAndLogin(t *testing.T, router http.Handler, email, password string) string {
	t.Helper()

	body, _ := json.Marshal(map[string]string{"email": email, "password": password})
	req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code, "register failed: %s", w.Body)

	var tokens auth.Tokens
	require.NoError(t, json.NewDecoder(w.Body).Decode(&tokens))
	return tokens.AccessToken
}

// authGet performs a GET with a Bearer token.
func authGet(t *testing.T, router http.Handler, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// authPost performs a POST with a Bearer token and JSON body.
func authPost(t *testing.T, router http.Handler, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// authDelete performs a DELETE with a Bearer token.
func authDelete(t *testing.T, router http.Handler, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}
