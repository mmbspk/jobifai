package handler_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/handler"
)

// Bot field is nil in these tests; the handlers gracefully degrade.

func TestBot_Status_NilBot_ReturnsIdle(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "botstatus@example.com", "password123")

	w := authGet(t, router, "/api/bot/status", token)
	assert.Equal(t, 200, w.Code)

	var status domain.BotStatus
	require.NoError(t, json.NewDecoder(w.Body).Decode(&status))
	assert.Equal(t, domain.BotStateIdle, status.State)
}

func TestBot_Start_NilBot_Returns503(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "botstart@example.com", "password123")

	w := authPost(t, router, "/api/bot/start", token, map[string]string{"platform": "linkedin"})
	assert.Equal(t, 503, w.Code)
}

func TestBot_Stop_NilBot_Returns503(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "botstop@example.com", "password123")

	w := authPost(t, router, "/api/bot/stop", token, nil)
	assert.Equal(t, 503, w.Code)
}

func TestBot_Status_RequiresAuth(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)

	w := authGet(t, router, "/api/bot/status", "")
	assert.Equal(t, 401, w.Code)
}
