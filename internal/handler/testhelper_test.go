package handler_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/domain"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/handler"
	"github.com/user/jobifai/internal/testutil/mockllm"
	"github.com/user/jobifai/internal/ws"
)

// newTestDB opens a fresh SQLite database with all migrations applied.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
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

// setUserAdmin promotes a user by email to admin (for handler tests).
func setUserAdmin(t *testing.T, db *sql.DB, email string) {
	t.Helper()
	_, err := db.Exec(`UPDATE users SET is_admin = 1 WHERE email = ?`, email)
	require.NoError(t, err)
}

// authPut performs a PUT with a Bearer token and JSON body.
func authPut(t *testing.T, router http.Handler, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, path, bytes.NewReader(b))
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

// authPostMultipart sends multipart/form-data with optional fields.
func authPostMultipart(t *testing.T, router http.Handler, path, token string, fields map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for k, v := range fields {
		require.NoError(t, w.WriteField(k, v))
	}
	require.NoError(t, w.Close())

	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// saveResumeProfile stores a profile via the settings API.
func saveResumeProfile(t *testing.T, router http.Handler, token string, profile domain.ResumeProfile) {
	t.Helper()
	w := authPost(t, router, "/api/settings/resume", token, profile)
	require.Equal(t, http.StatusOK, w.Code, "save profile: %s", w.Body.String())
}

// userIDFromToken returns the authenticated user's id.
func userIDFromToken(t *testing.T, router http.Handler, token string) string {
	t.Helper()
	w := authGet(t, router, "/api/me", token)
	require.Equal(t, http.StatusOK, w.Code)
	var me map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&me))
	return me["id"].(string)
}

// wireMockLLMUser stores proxy LLM settings + API key for userID (httptest server URL).
func wireMockLLMUser(t *testing.T, svc *handler.Services, userID, proxyURL string) {
	t.Helper()
	mockllm.StoreUserLLMConfig(t, svc.Config, svc.Secrets, userID, proxyURL)
}
