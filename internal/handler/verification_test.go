package handler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/email"
	"github.com/user/jobifai/internal/handler"
)

// buildRouterWithEmailSender wires an EmailSender and returns the chi router.
func buildRouterWithEmailSender(t *testing.T, sender handler.EmailSender) (http.Handler, *handler.Services) {
	t.Helper()
	svc, _ := newTestServices(t)
	svc.EmailSender = sender
	svc.AppBaseURL = "http://localhost:8081"
	return handler.NewRouter(svc), svc
}

func TestVerifyEmail_InvalidToken(t *testing.T) {
	router, _ := buildRouterWithEmailSender(t, email.NoopSender{})

	req := httptest.NewRequest(http.MethodGet, "/auth/verify-email?token=badtoken", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var body map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Contains(t, body["message"], "invalid")
}

func TestVerifyEmail_MissingToken(t *testing.T) {
	router, _ := buildRouterWithEmailSender(t, email.NoopSender{})

	req := httptest.NewRequest(http.MethodGet, "/auth/verify-email", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestVerifyEmail_ValidToken_VerifiesUser(t *testing.T) {
	capture := &email.CaptureSender{}
	router, svc := buildRouterWithEmailSender(t, capture)

	// Register a user — triggers async verification email.
	body, _ := json.Marshal(map[string]string{"email": "verify@example.com", "password": "password123"})
	req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	// Wait for the goroutine to deliver the email.
	rawToken := waitForEmail(t, capture)

	// Consume the token via the HTTP endpoint.
	req2 := httptest.NewRequest(http.MethodGet, "/auth/verify-email?token="+rawToken, nil)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusOK, w2.Code)

	// Check user is now verified in DB.
	user, err := svc.Users.ByEmail("verify@example.com")
	require.NoError(t, err)
	assert.True(t, user.EmailVerified)
}

func TestVerifyEmail_AlreadyUsed_Returns410(t *testing.T) {
	capture := &email.CaptureSender{}
	router, _ := buildRouterWithEmailSender(t, capture)

	body, _ := json.Marshal(map[string]string{"email": "used@example.com", "password": "password123"})
	req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	rawToken := waitForEmail(t, capture)
	require.NotEmpty(t, rawToken)

	// First use — success.
	req2 := httptest.NewRequest(http.MethodGet, "/auth/verify-email?token="+rawToken, nil)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusOK, w2.Code)

	// Second use — should return 410 Gone.
	req3 := httptest.NewRequest(http.MethodGet, "/auth/verify-email?token="+rawToken, nil)
	w3 := httptest.NewRecorder()
	router.ServeHTTP(w3, req3)
	assert.Equal(t, http.StatusGone, w3.Code)
}

func TestResendVerification_AlwaysReturns202(t *testing.T) {
	router, _ := buildRouterWithEmailSender(t, email.NoopSender{})

	// Known address — should return 202.
	body, _ := json.Marshal(map[string]string{"email": "nobody@example.com"})
	req := httptest.NewRequest(http.MethodPost, "/auth/resend-verification", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusAccepted, w.Code)
}

func TestResendVerification_MissingEmail_Returns400(t *testing.T) {
	router, _ := buildRouterWithEmailSender(t, email.NoopSender{})

	body, _ := json.Marshal(map[string]string{})
	req := httptest.NewRequest(http.MethodPost, "/auth/resend-verification", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestMe_ExposesEmailVerified(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)

	// Register a new user.
	regBody, _ := json.Marshal(map[string]string{"email": "me@example.com", "password": "password123"})
	req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(regBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)
	var tokens auth.Tokens
	require.NoError(t, json.NewDecoder(w.Body).Decode(&tokens))

	// GET /api/me should include email_verified = false.
	meReq := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	meW := httptest.NewRecorder()
	router.ServeHTTP(meW, meReq)
	require.Equal(t, http.StatusOK, meW.Code)
	var meBody map[string]any
	require.NoError(t, json.NewDecoder(meW.Body).Decode(&meBody))
	assert.Equal(t, false, meBody["email_verified"], "new email/password account should be unverified")

	// Manually verify the user via DB.
	us := auth.NewUserStore(db)
	user, _ := us.ByEmail("me@example.com")
	_, err := db.Exec(`UPDATE users SET email_verified = 1 WHERE id = ?`, user.ID)
	require.NoError(t, err)

	// GET /api/me should now show email_verified = true.
	meReq2 := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	meReq2.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	meW2 := httptest.NewRecorder()
	router.ServeHTTP(meW2, meReq2)
	require.Equal(t, http.StatusOK, meW2.Code)
	var meBody2 map[string]any
	require.NoError(t, json.NewDecoder(meW2.Body).Decode(&meBody2))
	assert.Equal(t, true, meBody2["email_verified"])
}

// waitForEmail blocks until CaptureSender has at least one email or timeout.
func waitForEmail(t *testing.T, capture *email.CaptureSender) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if capture.Count() > 0 {
			last := capture.Last()
			if last != nil {
				return extractTokenFromURL(last.VerifyURL)
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for verification email")
	return ""
}

// extractTokenFromURL parses the ?token= query param from a verify URL.
func extractTokenFromURL(verifyURL string) string {
	u, err := url.Parse(verifyURL)
	if err != nil {
		return ""
	}
	return u.Query().Get("token")
}
