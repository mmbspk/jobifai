package handler

import (
	"net/http"
	"strings"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// WSHandlers groups WebSocket handlers.
type WSHandlers struct{ svc *Services }

func NewWSHandlers(svc *Services) *WSHandlers { return &WSHandlers{svc: svc} }

// GET /ws/logs, live structured log stream.
// Accepts JWT via ?token= query param (browser WebSocket API cannot set headers)
// or via Authorization: Bearer header.
func (h *WSHandlers) Logs(w http.ResponseWriter, r *http.Request) {
	if h.svc.TokenManager != nil {
		tokenStr := r.URL.Query().Get("token")
		if tokenStr == "" {
			tokenStr = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		}
		if _, err := h.svc.TokenManager.Verify(tokenStr); err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	conn, err := websocket.Accept(w, r, wsOptions(r))
	if err != nil {
		return
	}
	defer conn.CloseNow()

	if h.svc.Logs != nil {
		h.svc.Logs.Register(conn)
		defer h.svc.Logs.Unregister(conn)
	} else {
		// Broadcaster not wired, send a single info message.
		_ = wsjson.Write(r.Context(), conn, map[string]string{
			"level":   "info",
			"message": "log stream connected (broadcaster not initialised)",
		})
	}

	// Block until client disconnects.
	for {
		if _, _, err := conn.Read(r.Context()); err != nil {
			return
		}
	}
}
