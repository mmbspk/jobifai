package handler_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/handler"
)

func TestUsers_Register_Success(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)

	w := authPost(t, router, "/auth/register", "", map[string]string{
		"email": "register@example.com", "password": "password123",
	})
	assert.Equal(t, 201, w.Code)

	var tokens auth.Tokens
	require.NoError(t, json.NewDecoder(w.Body).Decode(&tokens))
	assert.NotEmpty(t, tokens.AccessToken)
	assert.NotEmpty(t, tokens.RefreshToken)
}

func TestUsers_Register_DuplicateEmail(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)

	authPost(t, router, "/auth/register", "", map[string]string{
		"email": "dup@example.com", "password": "password123",
	})

	w := authPost(t, router, "/auth/register", "", map[string]string{
		"email": "dup@example.com", "password": "password456",
	})
	assert.Equal(t, 409, w.Code)
}

func TestUsers_Register_PasswordTooShort(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)

	w := authPost(t, router, "/auth/register", "", map[string]string{
		"email": "short@example.com", "password": "abc",
	})
	assert.Equal(t, 400, w.Code)
}

func TestUsers_Login_Success(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)

	authPost(t, router, "/auth/register", "", map[string]string{
		"email": "login@example.com", "password": "password123",
	})

	w := authPost(t, router, "/auth/login", "", map[string]string{
		"email": "login@example.com", "password": "password123",
	})
	assert.Equal(t, 200, w.Code)

	var tokens auth.Tokens
	require.NoError(t, json.NewDecoder(w.Body).Decode(&tokens))
	assert.NotEmpty(t, tokens.AccessToken)
}

func TestUsers_Login_WrongPassword(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)

	authPost(t, router, "/auth/register", "", map[string]string{
		"email": "wrongpw@example.com", "password": "password123",
	})

	w := authPost(t, router, "/auth/login", "", map[string]string{
		"email": "wrongpw@example.com", "password": "wrongpassword",
	})
	assert.Equal(t, 401, w.Code)
}

func TestUsers_Login_UnknownEmail(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)

	w := authPost(t, router, "/auth/login", "", map[string]string{
		"email": "nobody@example.com", "password": "password123",
	})
	assert.Equal(t, 401, w.Code)
}

func TestUsers_Me_Success(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "me@example.com", "password123")

	w := authGet(t, router, "/api/me", token)
	assert.Equal(t, 200, w.Code)

	var result map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.Equal(t, "me@example.com", result["email"])
}

func TestUsers_Me_NoToken(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)

	w := authGet(t, router, "/api/me", "")
	assert.Equal(t, 401, w.Code)
}
