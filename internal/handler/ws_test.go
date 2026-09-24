package handler_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/handler"
)

func TestWS_Logs_UnauthorizedWithoutToken(t *testing.T) {
	svc, _ := newTestServices(t)
	srv := httptest.NewServer(handler.NewRouter(svc))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	_, resp, err := websocket.Dial(ctx, wsURL(srv, "/ws/logs"), nil)
	require.Error(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 401, resp.StatusCode)
}

func TestWS_Logs_DeliversUserScopedLogLine(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "ws-user@example.com", "password123")
	userID := userIDFromToken(t, router, token)

	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	conn, _, err := websocket.Dial(ctx, wsURL(srv, "/ws/logs?token="+token), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.CloseNow() })

	line, err := json.Marshal(map[string]string{
		"level":   "info",
		"message": "automation heartbeat",
		"user_id": userID,
	})
	require.NoError(t, err)
	_, err = svc.Logs.Write(line)
	require.NoError(t, err)

	readCtx, readCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer readCancel()
	var payload map[string]any
	require.NoError(t, wsjson.Read(readCtx, conn, &payload))
	assert.Equal(t, "automation heartbeat", payload["message"])
}

func wsURL(srv *httptest.Server, path string) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http") + path
}
